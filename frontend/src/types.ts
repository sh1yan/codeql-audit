export type Severity = "error" | "warning" | "note" | "recommendation" | string;
export type Lang = "zh" | "en" | "both";
export type Theme = "dark" | "light";
export type Verdict = "false_positive" | "true_positive" | "uncertain" | "";

export interface LocationDTO {
  file: string;
  resolvedPath: string;
  startLine: number;
  startCol: number;
  endLine: number;
  endCol: number;
  label: string;
  labelZh: string;
  resolved: boolean;
  lineRange: string;
  columnRange: string;
  lineCol: string;
  displayPath: string;
  baseName: string;
}

export interface LinkDTO {
  label: string;
  labelZh: string;
  rawUri: string;
  file: string;
  startLine: number;
  startCol: number;
  endLine: number;
  endCol: number;
}

export interface SegmentDTO {
  kind: "text" | "link";
  text: string;
  link?: LinkDTO;
}

export interface AIDTO {
  verdict: Verdict;
  verdictZh: string;
  reason: string;
  confidence: number;
  model: string;
  at: string;
  short: string;
}

export interface FindingDTO {
  key: string;
  index: number;
  ruleName: string;
  ruleNameZh: string;
  ruleDisplay: string;
  description: string;
  descriptionZh: string;
  descriptionDisplay: string;
  severity: Severity;
  severityZh: string;
  sink: LocationDTO;
  sources: LocationDTO[];
  messageEn: SegmentDTO[];
  messageZh: SegmentDTO[];
  messageRaw: string;
  messageKey: string;
  messageZhTemplate: string;
  translateSource: string;
  translateSourceZh: string;
  translatePending: boolean;
  reviewed: boolean;
  ai: AIDTO | null;
}

export interface StatsDTO {
  total: number;
  ruleKinds: number;
  fileKinds: number;
  resolved: number;
  missing: number;
  unread: number;
  pending: number;
  verified: number;
  falsePositive: number;
  truePositive: number;
  uncertain: number;
  translationBuiltin: number;
  translationStore: number;
  translationLLM: number;
  bySeverity: Record<string, number>;
  totalSourceLocation: number;
}

export interface OptionDTO {
  value: string;
  label: string;
  count: number;
}

export interface UnresolvedDTO {
  file: string;
  count: number;
  severities: Severity[];
}

export interface PrefixRuleDTO {
  from: string;
  to: string;
}

export interface ProjectDTO {
  csvPath: string;
  csvName: string;
  srcRoot: string;
  findings: FindingDTO[];
  stats: StatsDTO;
  rules: OptionDTO[];
  files: OptionDTO[];
  severities: OptionDTO[];
  warnings: string[];
  unresolved: UnresolvedDTO[];
  prefixRules: PrefixRuleDTO[];
  overrides: Record<string, string>;
  loadedAt: string;
  translationDb: string;
}

export interface LLMSettingsDTO {
  enabled: boolean;
  baseUrl: string;
  apiKey: string;
  model: string;
  timeoutSeconds: number;
  templateOnly: boolean;
}

export interface EditorSettingsDTO {
  cmd: string;
  args: string;
}

export interface SettingsDTO {
  llm: LLMSettingsDTO;
  aiVerifyEnabled: boolean;
  aiSendCode: boolean;
  editor: EditorSettingsDTO;
  displayLang: Lang;
  showColumnRuler: boolean;
  codeSearchShortcut: string;
  translateDb: string;
  dataDir: string;
  configPath: string;
  windowWidth: number;
  windowHeight: number;
  splitLeft: number;
  splitDetail: number;
  topInfoCollapsed: boolean;
}

export interface BootstrapDTO {
  settings: SettingsDTO;
  lastProject: string;
  lastSrcRoot: string;
  autoLoad: boolean;
  platform: string;
  version: string;
  defaultSearchShortcut: string;
  defaultEditorArgs: string;
  dataDir: string;
  configPath: string;
  warning?: string;
}

export interface FileDTO {
  path: string;
  displayPath: string;
  encoding: string;
  lines: string[];
  totalLines: number;
}

export interface VerifyResponseDTO {
  findingKey: string;
  ai: AIDTO | null;
  stats: StatsDTO;
}

export interface BatchProgressDTO {
  done: number;
  total: number;
  falsePositive: number;
  truePositive: number;
  uncertain: number;
  failed: number;
  lastError?: string;
  findingKey?: string;
  ai?: AIDTO | null;
}

export interface BatchSummaryDTO {
  done: number;
  total: number;
  falsePositive: number;
  truePositive: number;
  uncertain: number;
  failed: number;
  stopped: boolean;
  message: string;
}

export interface BatchStartDTO {
  total: number;
}

export interface LibraryEntryDTO {
  id: number;
  kind: string;
  kindZh: string;
  sourceEn: string;
  sourceZh: string;
  origin: string;
  originZh: string;
  hits: number;
  updatedAt: string;
}

export interface LibraryDTO {
  total: number;
  shown: number;
  entries: LibraryEntryDTO[];
}

export interface MappingDTO {
  unresolved: UnresolvedDTO[];
  prefixRules: PrefixRuleDTO[];
  overrides: Record<string, string>;
  totalFiles: number;
  totalFindings: number;
  resolved: number;
  stats: StatsDTO;
}

export interface ExportResultDTO {
  path: string;
  count: number;
  kind: string;
}

export interface TestConnectionDTO {
  endpoint: string;
  model: string;
  reply: string;
}

export interface UIPrefsDTO {
  displayLang: Lang;
  showColumnRuler: boolean;
  splitLeft: number;
  splitDetail: number;
  windowWidth: number;
  windowHeight: number;
  topInfoCollapsed: boolean;
}

export interface AppApi {
  Bootstrap(): Promise<BootstrapDTO>;
  GetSettings(): Promise<SettingsDTO>;
  SaveSettings(s: SettingsDTO): Promise<SettingsDTO>;
  SaveUIPreferences(p: UIPrefsDTO): Promise<void>;
  SetNativeTheme(theme: string): Promise<void>;
  ChooseCSVFile(): Promise<string>;
  ChooseDirectory(title: string): Promise<string>;
  LoadProject(csvPath: string, srcRoot: string): Promise<ProjectDTO>;
  GetProject(): Promise<ProjectDTO | null>;
  SetSrcRoot(root: string): Promise<ProjectDTO>;
  GetSourceFile(csvPath: string, resolvedPath: string): Promise<FileDTO>;
  ResolvePath(csvPath: string): Promise<string>;
  SetReviewed(keys: string[], reviewed: boolean): Promise<StatsDTO>;
  ResetReviewed(): Promise<StatsDTO>;
  GetMapping(): Promise<MappingDTO>;
  AddPrefixRule(from: string, to: string): Promise<MappingDTO>;
  RemovePrefixRule(index: number): Promise<MappingDTO>;
  ChooseOverrideFile(csvPath: string): Promise<MappingDTO>;
  RemoveOverride(csvPath: string): Promise<MappingDTO>;
  OpenInEditor(path: string, line: number, col: number): Promise<string>;
  RevealInFinder(path: string): Promise<void>;
  CopyText(text: string): Promise<void>;
  OpenExternalURL(url: string): Promise<void>;
  VerifyFinding(key: string): Promise<VerifyResponseDTO>;
  ClearVerdict(key: string): Promise<StatsDTO>;
  StartBatchVerify(keys: string[]): Promise<BatchStartDTO>;
  CancelBatchVerify(): Promise<void>;
  TestConnection(s: LLMSettingsDTO): Promise<TestConnectionDTO>;
  GetLibrary(kind: string, query: string): Promise<LibraryDTO>;
  SaveLibraryEntry(kind: string, en: string, zh: string): Promise<void>;
  CorrectMessage(key: string, template: string): Promise<{
    finding: FindingDTO;
    stats: StatsDTO;
  }>;
  ExportChineseCSV(keys: string[]): Promise<ExportResultDTO | null>;
  ExportAuditReport(keys: string[]): Promise<ExportResultDTO | null>;
  ExportVerdicts(): Promise<ExportResultDTO | null>;
}

export interface ActiveLocation {
  kind: "sink" | "source" | "link";
  index: number;
  loc: LocationDTO;
}

export interface NavPoint {
  csvPath: string;
  resolvedPath: string;
  line: number;
  active: ActiveLocation;
}
