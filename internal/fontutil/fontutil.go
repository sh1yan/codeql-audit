// Package fontutil 负责挑选并准备可用的中文字体。
//
// 背景：Fyne 通过 go-text/typesetting 的 font.ParseTTF 加载主题字体，而该系统
// 只接受单一字体文件；中文系统上的字体多数是 .ttc/.otc 集合（macOS 的
// Songti/Hiragino/STHeiti、Windows 的 msyh、Linux 的 NotoSansCJK），
// 直接丢给 ParseTTF 会解析失败甚至 panic。因此这里实现集合抽取，
// 把 .ttc 中的某个字重还原成独立的单字体数据。
package fontutil

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/go-text/typesetting/font"
)

// ttcTag 是字体集合的标志 'ttcf'。
const ttcTag = 0x74746366

// sfntVersionTrueType / OpenType(CFF)
const (
	sfntTrueType = 0x00010000
	sfntOTTO     = 0x4F54544F
)

// IsCollection 判断数据是否为字体集合（.ttc/.otc）。
func IsCollection(data []byte) bool {
	return len(data) >= 4 && binary.BigEndian.Uint32(data[:4]) == ttcTag
}

// CollectionSize 返回集合中的字体数量；非集合返回 0。
func CollectionSize(data []byte) int {
	if !IsCollection(data) || len(data) < 12 {
		return 0
	}
	return int(binary.BigEndian.Uint32(data[8:12]))
}

// ExtractFace 从字体集合中提取第 index 个字重，返回独立的单字体数据。
// 输入不是集合时原样返回。
func ExtractFace(data []byte, index int) ([]byte, error) {
	if !IsCollection(data) {
		return data, nil
	}
	if len(data) < 12 {
		return nil, fmt.Errorf("字体集合头部不完整")
	}
	n := int(binary.BigEndian.Uint32(data[8:12]))
	if n <= 0 {
		return nil, fmt.Errorf("字体集合中没有字体")
	}
	if index < 0 || index >= n {
		index = 0
	}
	// 版本 2 的头部在偏移表之后还有 dsig 字段，但偏移表位置固定
	offTable := 12
	if len(data) < offTable+4*n {
		return nil, fmt.Errorf("字体集合偏移表不完整")
	}
	faceOff := int(binary.BigEndian.Uint32(data[offTable+4*index : offTable+4*index+4]))
	if faceOff <= 0 || faceOff+12 > len(data) {
		return nil, fmt.Errorf("字体集合中的偏移越界")
	}

	// 读取该字重的 sfnt 目录
	p := faceOff
	sfntVersion := binary.BigEndian.Uint32(data[p : p+4])
	if sfntVersion != sfntTrueType && sfntVersion != sfntOTTO && sfntVersion != 0x74727565 {
		return nil, fmt.Errorf("未知的 sfnt 版本 0x%08x", sfntVersion)
	}
	numTables := int(binary.BigEndian.Uint16(data[p+4 : p+6]))
	searchRange := binary.BigEndian.Uint16(data[p+6 : p+8])
	entrySelector := binary.BigEndian.Uint16(data[p+8 : p+10])
	rangeShift := binary.BigEndian.Uint16(data[p+10 : p+12])

	type tableRec struct {
		tag      uint32
		checksum uint32
		data     []byte
	}
	recs := make([]tableRec, 0, numTables)
	recStart := p + 12
	if recStart+16*numTables > len(data) {
		return nil, fmt.Errorf("字体表目录不完整")
	}
	for i := 0; i < numTables; i++ {
		b := recStart + 16*i
		tag := binary.BigEndian.Uint32(data[b : b+4])
		checksum := binary.BigEndian.Uint32(data[b+4 : b+8])
		off := int(binary.BigEndian.Uint32(data[b+8 : b+12]))
		length := int(binary.BigEndian.Uint32(data[b+12 : b+16]))
		if off < 0 || length < 0 || off+length > len(data) {
			return nil, fmt.Errorf("字体表 %08x 越界", tag)
		}
		recs = append(recs, tableRec{tag: tag, checksum: checksum, data: data[off : off+length]})
	}

	// 重新组装为独立的单字体文件
	headSize := 12 + 16*numTables
	out := make([]byte, 0, headSize+1024*len(recs))
	var head [12]byte
	binary.BigEndian.PutUint32(head[0:4], sfntVersion)
	binary.BigEndian.PutUint16(head[4:6], uint16(numTables))
	binary.BigEndian.PutUint16(head[6:8], searchRange)
	binary.BigEndian.PutUint16(head[8:10], entrySelector)
	binary.BigEndian.PutUint16(head[10:12], rangeShift)
	out = append(out, head[:]...)

	dataOff := headSize
	records := make([]byte, 0, 16*numTables)
	body := make([]byte, 0, 1<<20)
	for _, r := range recs {
		var rec [16]byte
		binary.BigEndian.PutUint32(rec[0:4], r.tag)
		binary.BigEndian.PutUint32(rec[4:8], r.checksum)
		binary.BigEndian.PutUint32(rec[8:12], uint32(dataOff))
		binary.BigEndian.PutUint32(rec[12:16], uint32(len(r.data)))
		records = append(records, rec[:]...)
		body = append(body, r.data...)
		// 每张表按 4 字节对齐
		if pad := (4 - len(r.data)%4) % 4; pad > 0 {
			body = append(body, make([]byte, pad)...)
		}
		dataOff += len(r.data) + (4-len(r.data)%4)%4
	}
	out = append(out, records...)
	out = append(out, body...)
	return out, nil
}

// GlyphCheck 描述一个字体对关键字符的支持情况。
type GlyphCheck struct {
	OK        bool // 至少能显示汉字
	Han       bool
	Latin     bool
	FullWidth bool // 全角标点（中文排版常用）
}

// Check 解析字体数据，检查它是否覆盖中文界面所需的关键字符。
//
// go-text 在个别 CFF2 字体上会直接 panic（不是返回 error），
// 而字体选择要遍历系统上的大量字体文件，因此这里必须拦截 panic。
func Check(data []byte) (chk GlyphCheck) {
	defer func() {
		if r := recover(); r != nil {
			chk = GlyphCheck{}
		}
	}()
	face, err := font.ParseTTF(bytes.NewReader(data))
	if err != nil {
		return GlyphCheck{}
	}
	_, han := face.NominalGlyph('中')
	_, latin := face.NominalGlyph('A')
	_, full := face.NominalGlyph('，')
	return GlyphCheck{OK: han && latin, Han: han, Latin: latin, FullWidth: full}
}

// CheckFile 读取字体文件（必要时抽取字重）并检查中文字形覆盖。
// 返回可用于 Fyne 主题的字体数据。
func CheckFile(data []byte) ([]byte, GlyphCheck) {
	if IsCollection(data) {
		if extracted, err := ExtractFace(data, 0); err == nil {
			if c := Check(extracted); c.OK {
				return extracted, c
			}
		}
		// 集合里第一个字重不可用时，逐个尝试
		if n := CollectionSize(data); n > 1 {
			for i := 1; i < n; i++ {
				if extracted, err := ExtractFace(data, i); err == nil {
					if c := Check(extracted); c.OK {
						return extracted, c
					}
				}
			}
		}
		return nil, GlyphCheck{}
	}
	return data, Check(data)
}
