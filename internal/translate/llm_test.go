package translate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// mockLLM 模拟 OpenAI 兼容的 chat/completions 接口。
// 返回的译文保留占位符，用于验证「大模型结果能落库并被复用」。
func mockLLM(t *testing.T, calls *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
			return
		}
		var req struct {
			Messages []struct {
				Role, Content string
			} `json:"messages"`
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		user := ""
		if len(req.Messages) > 0 {
			user = req.Messages[len(req.Messages)-1].Content
		}
		// 模拟模型行为：保留占位符，翻译固定句式；无占位符时加个前缀便于断言
		zh := "【大模型译文】" + strings.ReplaceAll(user, "with untrusted data", "携带不可信数据")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": zh}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newLLMEngine(t *testing.T, baseURL string) *Engine {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "llm.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	e, err := NewEngine(store, NewLLMClient(LLMConfig{
		Enabled:        true,
		BaseURL:        baseURL,
		APIKey:         "test-key",
		Model:          "mock-model",
		TimeoutSeconds: 10,
		TemplateOnly:   true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestLLMEndpoint(t *testing.T) {
	cases := map[string]string{
		"https://api.deepseek.com/v1":                "https://api.deepseek.com/v1/chat/completions",
		"https://api.deepseek.com/v1/":               "https://api.deepseek.com/v1/chat/completions",
		"http://localhost:11434/v1/chat/completions": "http://localhost:11434/v1/chat/completions",
	}
	for in, want := range cases {
		if got := (LLMConfig{BaseURL: in}).Endpoint(); got != want {
			t.Errorf("Endpoint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLLMConfigValid(t *testing.T) {
	if (LLMConfig{Enabled: false, BaseURL: "x", Model: "y"}).Valid() {
		t.Error("未启用时不应有效")
	}
	if (LLMConfig{Enabled: true, BaseURL: "", Model: "y"}).Valid() {
		t.Error("缺少地址时不应有效")
	}
	if (LLMConfig{Enabled: true, BaseURL: "x", Model: ""}).Valid() {
		t.Error("缺少模型时不应有效")
	}
	if !(LLMConfig{Enabled: true, BaseURL: "x", Model: "y"}).Valid() {
		t.Error("配置齐全时应有效")
	}
}

// TestLLMFallbackAndCache 验证：库里没有才调用大模型，且结果落库后可离线复用。
func TestLLMFallbackAndCache(t *testing.T) {
	var calls int32
	srv := mockLLM(t, &calls)
	e := newLLMEngine(t, srv.URL)

	// 内置词典能命中的内容不应触发网络请求
	if got := e.TranslateStatic(KindRule, "Hard-coded credentials").ZH; got != "硬编码凭据" {
		t.Fatalf("内置译文 = %q", got)
	}
	if calls != 0 {
		t.Fatalf("命中内置词典时不应调用大模型，实际调用 %d 次", calls)
	}

	// 完全陌生的规则名 -> 走大模型
	const unknown = "Totally Unknown Security Query About Frobnication"
	r := e.TranslateStatic(KindRule, unknown)
	if r.Pending {
		t.Fatal("配置了大模型后不应标记为待翻译")
	}
	if r.Source != SourceLLM {
		t.Fatalf("来源 = %q, want %q", r.Source, SourceLLM)
	}
	if !strings.Contains(r.ZH, "【大模型译文】") {
		t.Fatalf("未取到大模型译文: %q", r.ZH)
	}
	if calls != 1 {
		t.Fatalf("应只调用 1 次，实际 %d", calls)
	}

	// 第二次必须命中翻译库，不再发请求
	r2 := e.TranslateStatic(KindRule, unknown)
	if calls != 1 {
		t.Fatalf("第二次应命中翻译库，实际调用 %d 次", calls)
	}
	if r2.ZH != r.ZH {
		t.Fatalf("缓存译文不一致: %q vs %q", r2.ZH, r.ZH)
	}

	// 换一个全新引擎（模拟重启），译文应来自 SQLite 而非网络
	e2 := newLLMEngine(t, srv.URL)
	e2.store = e.store // 复用同一个库
	r3 := e2.TranslateStatic(KindRule, unknown)
	if calls != 1 {
		t.Fatalf("重启后应从翻译库读到译文，实际调用 %d 次", calls)
	}
	if r3.ZH != r.ZH {
		t.Fatalf("库中译文不一致: %q vs %q", r3.ZH, r.ZH)
	}
}

// TestLLMMessagePreservesPlaceholders 验证模板消息在过大模型后仍保留占位符。
func TestLLMMessagePreservesPlaceholders(t *testing.T) {
	var calls int32
	srv := mockLLM(t, &calls)
	e := newLLMEngine(t, srv.URL)

	key := "A frobnicator call to {{API}} with untrusted data from {{LINK}}."
	r := e.TranslateMessage(key)
	if r.Pending {
		t.Fatal("不应待翻译")
	}
	if !strings.Contains(r.ZH, PhAPI) || !strings.Contains(r.ZH, PhLink) {
		t.Fatalf("译文丢失占位符: %q", r.ZH)
	}
	// 落库的键必须是规范化模板，下次才能命中
	if _, _, ok := e.store.Lookup(KindMessage, key); !ok {
		t.Fatal("大模型译文未按模板键入库")
	}
}

// TestLLMErrors 验证网络错误被安全处理，不会 panic，也不会写入脏数据。
func TestLLMErrors(t *testing.T) {
	// 401
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()
	e := newLLMEngine(t, srv.URL)
	r := e.TranslateStatic(KindRule, "Some Unknown Rule Name For Failure Test")
	if !r.Pending {
		t.Error("调用失败时应标记为待翻译")
	}
	if r.ZH != "Some Unknown Rule Name For Failure Test" {
		t.Errorf("失败时应回退英文原文，实际 %q", r.ZH)
	}
	if _, _, ok := e.store.Lookup(KindRule, "Some Unknown Rule Name For Failure Test"); ok {
		t.Error("失败的翻译不应写入翻译库")
	}

	// 连接不上
	e2 := newLLMEngine(t, "http://127.0.0.1:1")
	ctx, cancel := context.WithTimeout(context.Background(), 2e9)
	defer cancel()
	if _, err := e2.LLM().Translate(ctx, KindRule, "x"); err == nil {
		t.Error("连接失败时应返回错误")
	}
}

// TestLLMReturnsEmpty 空译文不应写库。
func TestLLMReturnsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"   "}}]}`)
	}))
	defer srv.Close()
	e := newLLMEngine(t, srv.URL)
	const unknown = "Another Unknown Rule For Empty Response Test"
	r := e.TranslateStatic(KindRule, unknown)
	if !r.Pending {
		t.Error("空译文应视为失败")
	}
	if _, _, ok := e.store.Lookup(KindRule, unknown); ok {
		t.Error("空译文不应入库")
	}
}

// TestCleanLLMOutput 验证模型输出里的代码块围栏与引号被清掉。
func TestCleanLLMOutput(t *testing.T) {
	cases := map[string]string{
		"```\n译文\n```":     "译文",
		"```json\n译文\n```": "译文",
		`"译文"`:             "译文",
		"“译文”":             "译文",
		"  译文  ":           "译文",
		"保留 {{LINK}} 的译文":  "保留 {{LINK}} 的译文",
	}
	for in, want := range cases {
		if got := cleanLLMOutput(in); got != want {
			t.Errorf("cleanLLMOutput(%q) = %q, want %q", in, got, want)
		}
	}
}
