package config

import (
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog/log"
)

// LLMProvider identifies which backend to route LLM requests to.
type LLMProvider string

const (
	ProviderGroq   LLMProvider = "groq"
	ProviderOllama LLMProvider = "ollama"
	ProviderOpenAI LLMProvider = "openai"
	ProviderCustom LLMProvider = "custom"
)

// Config holds all runtime configuration loaded from environment variables.
// All fields map 1-to-1 with the original Python pydantic Settings class,
// plus new fields for the Go stack (LiveKit, Deepgram, Groq, Ollama).
type Config struct {
	// ── LLM ────────────────────────────────────────────────────────────
	// Set LLM_BASE_URL to switch provider:
	//   Groq:   https://api.groq.com/openai/v1   (LLM_API_KEY = GROQ_API_KEY)
	//   Ollama: http://localhost:11434/v1          (no key needed)
	//   OpenAI: https://api.openai.com/v1         (LLM_API_KEY = OPENAI_API_KEY)
	LLMAPIKey      string
	LLMBaseURL     string
	LLMProvider    LLMProvider // derived from LLMBaseURL
	CuratorModel   string
	CuratorFallback string
	ToolPlannerModel string
	VisionModel    string // model used for frame analysis (supports images)

	// ── Groq-specific ──────────────────────────────────────────────────
	GroqAPIKey string // GROQ_API_KEY — used when LLM_BASE_URL not set

	// ── Ollama-specific ────────────────────────────────────────────────
	OllamaBaseURL   string // default: http://localhost:11434/v1
	OllamaTextModel string // default: llama3.2:3b  (CPU-safe)
	OllamaVisionModel string // default: qwen2.5-vl:7b (needs ≥16GB RAM)

	// ── Notion ─────────────────────────────────────────────────────────
	NotionToken        string
	NotionParentPageID string
	NotionVersion      string
	NotionBatchInterval time.Duration

	// ── LiveKit (replaces VideoDB capture) ─────────────────────────────
	LiveKitURL       string // wss://your-livekit-server or LiveKit Cloud URL
	LiveKitAPIKey    string
	LiveKitAPISecret string

	// ── Deepgram (replaces VideoDB transcript) ─────────────────────────
	DeepgramAPIKey string
	// When set, use Whisper.cpp server instead of Deepgram.
	WhisperServerURL string // e.g. http://localhost:9000

	// ── Server ─────────────────────────────────────────────────────────
	BackendHost string
	BackendPort int

	// ── Session / pipeline tuning ──────────────────────────────────────
	CuratorInitialWindowSeconds int
	CuratorWindowSeconds        int
	ScreenshotLocalFallback     bool
	ScreenshotLocalFirst        bool

	// ── Visual indexing ────────────────────────────────────────────────
	VisualIndexPrompts string // "||" separated prompts
	AudioIndexModel    string

	// ── Capture token TTL (kept for compat) ────────────────────────────
	CaptureClientTokenTTL time.Duration
}

// Load reads the .env file (if present) and environment variables,
// returning a fully populated Config. Panics on required fields being absent.
func Load() *Config {
	// Load .env from parent directory (mirrors Python: env_file="../.env")
	if err := godotenv.Load("../.env"); err != nil {
		log.Debug().Msg("No ../.env file found, relying on environment variables")
	}
	// Also try current directory .env
	_ = godotenv.Overload(".env")

	cfg := &Config{
		// LLM
		LLMAPIKey:        firstNonEmpty(os.Getenv("LLM_API_KEY"), os.Getenv("OPENAI_API_KEY"), os.Getenv("GROQ_API_KEY")),
		LLMBaseURL:       firstNonEmpty(os.Getenv("LLM_BASE_URL"), os.Getenv("OPENAI_BASE_URL")),
		CuratorModel:     getEnvDefault("CURATOR_MODEL", "llama-3.3-70b-versatile"),
		CuratorFallback:  getEnvDefault("CURATOR_FALLBACK_MODEL", "llama3-8b-8192"),
		ToolPlannerModel: getEnvDefault("TOOL_PLANNER_MODEL", "llama-3.3-70b-versatile"),
		VisionModel:      getEnvDefault("VISION_MODEL", "meta-llama/llama-4-scout-17b-16e-instruct"),

		// Groq
		GroqAPIKey: os.Getenv("GROQ_API_KEY"),

		// Ollama
		OllamaBaseURL:     getEnvDefault("OLLAMA_BASE_URL", "http://localhost:11434/v1"),
		OllamaTextModel:   getEnvDefault("OLLAMA_TEXT_MODEL", "llama3.2:3b"),
		OllamaVisionModel: getEnvDefault("OLLAMA_VISION_MODEL", "qwen2.5-vl:7b"),

		// Notion
		NotionToken:         mustGetEnv("NOTION_TOKEN"),
		NotionParentPageID:  parseNotionID(mustGetEnv("NOTION_PARENT_PAGE_ID")),
		NotionVersion:       getEnvDefault("NOTION_VERSION", "2022-06-28"),
		NotionBatchInterval: parseDurationSeconds(os.Getenv("NOTION_BATCH_INTERVAL_S"), 4*time.Second),

		// LiveKit
		LiveKitURL:       getEnvDefault("LIVEKIT_URL", "ws://localhost:7880"),
		LiveKitAPIKey:    os.Getenv("LIVEKIT_API_KEY"),
		LiveKitAPISecret: os.Getenv("LIVEKIT_API_SECRET"),

		// Deepgram / Whisper
		DeepgramAPIKey:  os.Getenv("DEEPGRAM_API_KEY"),
		WhisperServerURL: os.Getenv("WHISPER_SERVER_URL"),

		// Server
		BackendHost: getEnvDefault("BACKEND_HOST", "127.0.0.1"),
		BackendPort: getEnvInt("BACKEND_PORT", 8000),

		// Pipeline tuning
		CuratorInitialWindowSeconds: getEnvInt("CURATOR_INITIAL_WINDOW_SECONDS", 12),
		CuratorWindowSeconds:        getEnvInt("CURATOR_WINDOW_SECONDS", 30),
		ScreenshotLocalFallback:     getEnvBool("SCREENSHOT_LOCAL_FALLBACK", true),
		ScreenshotLocalFirst:        getEnvBool("SCREENSHOT_LOCAL_FIRST", false),
		VisualIndexPrompts:          os.Getenv("VISUAL_INDEX_PROMPTS"),
		AudioIndexModel:             getEnvDefault("AUDIO_INDEX_MODEL", ""),

		// Capture TTL
		CaptureClientTokenTTL: time.Duration(getEnvInt("CAPTURE_CLIENT_TOKEN_TTL_SECONDS", 1800)) * time.Second,
	}

	cfg.LLMProvider = cfg.detectProvider()
	cfg.applyProviderDefaults()

	return cfg
}

// detectProvider derives the LLMProvider from LLMBaseURL or explicit API keys.
func (c *Config) detectProvider() LLMProvider {
	switch {
	case c.LLMBaseURL == "" && c.GroqAPIKey != "":
		// Groq key set, no explicit base URL → use Groq
		c.LLMBaseURL = "https://api.groq.com/openai/v1"
		if c.LLMAPIKey == "" {
			c.LLMAPIKey = c.GroqAPIKey
		}
		return ProviderGroq
	case containsAny(c.LLMBaseURL, "groq.com"):
		if c.LLMAPIKey == "" {
			c.LLMAPIKey = c.GroqAPIKey
		}
		return ProviderGroq
	case containsAny(c.LLMBaseURL, "localhost:11434", "ollama"):
		return ProviderOllama
	case c.LLMBaseURL == "" && c.LLMAPIKey != "":
		c.LLMBaseURL = "https://api.openai.com/v1"
		return ProviderOpenAI
	case c.LLMBaseURL != "":
		return ProviderCustom
	default:
		return ProviderOllama // fully local default
	}
}

// applyProviderDefaults sets sensible model defaults per provider.
func (c *Config) applyProviderDefaults() {
	if c.LLMProvider == ProviderOllama {
		// Ollama CPU-safe defaults
		if c.CuratorModel == "llama-3.3-70b-versatile" {
			c.CuratorModel = c.OllamaTextModel
		}
		if c.CuratorFallback == "llama3-8b-8192" {
			c.CuratorFallback = c.OllamaTextModel
		}
		if c.ToolPlannerModel == "llama-3.3-70b-versatile" {
			c.ToolPlannerModel = c.OllamaTextModel
		}
		if c.VisionModel == "meta-llama/llama-4-scout-17b-16e-instruct" {
			c.VisionModel = c.OllamaVisionModel
		}
	}
}

// UseOllamaForVision returns true when vision inference should use Ollama.
func (c *Config) UseOllamaForVision() bool {
	return c.LLMProvider == ProviderOllama
}

// UseDeepgram returns true when Deepgram is the STT backend.
func (c *Config) UseDeepgram() bool {
	return c.WhisperServerURL == "" && c.DeepgramAPIKey != ""
}

// VisualPrompts returns the list of prompts for visual frame analysis.
func (c *Config) VisualPrompts() []string {
	if c.VisualIndexPrompts == "" {
		return []string{
			"Describe learning-relevant visual content only: code, diagrams, charts, formulas, " +
				"slides, whiteboard, or tutorial UI steps.",
		}
	}
	var prompts []string
	for _, p := range splitTrim(c.VisualIndexPrompts, "||") {
		if p != "" {
			prompts = append(prompts, p)
		}
	}
	return prompts
}

// ── helpers ───────────────────────────────────────────────────────────────────

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func mustGetEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Warn().Str("key", key).Msg("Required environment variable not set")
	}
	return v
}

func getEnvInt(key string, def int) int {
	s := os.Getenv(key)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

func getEnvBool(key string, def bool) bool {
	s := os.Getenv(key)
	if s == "" {
		return def
	}
	v, err := strconv.ParseBool(s)
	if err != nil {
		return def
	}
	return v
}

func parseDurationSeconds(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return time.Duration(f * float64(time.Second))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func containsAny(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if len(s) > 0 && len(sub) > 0 {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

func splitTrim(s, sep string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			out = append(out, trimSpace(s[start:i]))
			start = i + len(sep)
		}
	}
	out = append(out, trimSpace(s[start:]))
	return out
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}

func parseNotionID(val string) string {
	// Find the first 32-character hex sequence (optionally dashed)
	re := regexp.MustCompile(`([a-fA-F0-9]{8}-?[a-fA-F0-9]{4}-?[a-fA-F0-9]{4}-?[a-fA-F0-9]{4}-?[a-fA-F0-9]{12})|([a-fA-F0-9]{32})`)
	match := re.FindString(val)
	if match != "" {
		// If it's the 32-char undashed version, we can format it to uuid, or Notion just accepts the 32-char string.
		// Notion API accepts the 32-char string without dashes as a page ID.
		return match
	}
	return val
}
