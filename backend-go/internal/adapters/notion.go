package adapters

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rs/zerolog/log"
	"github.com/sangeetmore/novo-transcriber/backend-go/internal/config"
	"github.com/sangeetmore/novo-transcriber/backend-go/internal/models"
)

const (
	maxTextLen       = 1800
	defaultNoteTopic = "Video Notes"
	notionAPIBase    = "https://api.notion.com/v1"
)

var badImageCaptionPhrases = []string{
	"no visual content",
	"no visual information",
	"nothing to screenshot",
	"nothing useful to capture",
	"nothing useful to screenshot",
	"nothing to capture",
	"transcript alone is sufficient",
	"audio alone is sufficient",
}

// ── helpers ───────────────────────────────────────────────────────────────────

// splitForNotion splits text on word boundaries so that each chunk is at most
// maxLen bytes. It mirrors the Python _split_for_notion function exactly:
// collapse internal whitespace first, then cut at the last space within the
// limit, falling back to a hard cut when no space is found.
func splitForNotion(text string, maxLen int) []string {
	clean := strings.TrimSpace(strings.Join(strings.Fields(text), " "))
	if clean == "" {
		return nil
	}
	if len(clean) <= maxLen {
		return []string{clean}
	}
	var out []string
	rest := clean
	for len(rest) > maxLen {
		cut := strings.LastIndex(rest[:maxLen], " ")
		if cut <= 0 {
			cut = maxLen
		}
		out = append(out, strings.TrimSpace(rest[:cut]))
		rest = strings.TrimSpace(rest[cut:])
	}
	if rest != "" {
		out = append(out, rest)
	}
	return out
}

// richText builds a Notion rich_text array with a single plain-text element.
func richText(text string) []map[string]any {
	return []map[string]any{
		{"type": "text", "text": map[string]any{"content": text}},
	}
}

// cleanImageCaption normalises caption whitespace, rejects captions that
// contain any of the badImageCaptionPhrases, and truncates to 180 runes.
func cleanImageCaption(caption string) string {
	clean := strings.TrimSpace(strings.Join(strings.Fields(caption), " "))
	if clean == "" {
		return ""
	}
	lower := strings.ToLower(clean)
	for _, phrase := range badImageCaptionPhrases {
		if strings.Contains(lower, phrase) {
			return ""
		}
	}
	runes := []rune(clean)
	if len(runes) > 180 {
		return string(runes[:180])
	}
	return clean
}

// cleanTopic normalises the topic string: collapse whitespace, strip well-known
// bad prefixes (same set as Python), truncate to 80 runes.
// Returns defaultNoteTopic when the result is empty.
func cleanTopic(topic string) string {
	clean := strings.TrimSpace(strings.Join(strings.Fields(topic), " "))
	if clean == "" {
		return defaultNoteTopic
	}
	badPrefixes := []string{"tutorial:", "lecture:", "video about", "introduction to"}
	lower := strings.ToLower(clean)
	for _, prefix := range badPrefixes {
		if strings.HasPrefix(lower, prefix) {
			clean = strings.TrimLeftFunc(clean[len(prefix):], func(r rune) bool {
				return unicode.IsSpace(r) || r == ':' || r == '-'
			})
			break
		}
	}
	runes := []rune(clean)
	if len(runes) > 80 {
		clean = string(runes[:80])
	}
	if clean == "" {
		return defaultNoteTopic
	}
	return clean
}

// FormatNoteTitle returns a human-readable page title:
//
//	"NoVo Transcriber · <cleanTopic> (<MonthDay> · HH:MM)"
func FormatNoteTitle(topic string) string {
	now := time.Now()
	monthDay := fmt.Sprintf("%s %d", now.Format("Jan"), now.Day())
	return fmt.Sprintf("NoVo Transcriber · %s (%s · %s)", cleanTopic(topic), monthDay, now.Format("15:04"))
}

func blockHash(block map[string]any) string {
	b, _ := json.Marshal(block)
	h := sha1.Sum(b)
	return fmt.Sprintf("%x", h)
}

// ── NotionWriter ──────────────────────────────────────────────────────────────

// NotionWriter queues and flushes blocks to a Notion page.
// It is a direct port of the Python NotionWriter class.
type NotionWriter struct {
	httpClient  *http.Client
	token       string
	version     string
	cfg         *config.Config

	// PageID and PageURL are set by CreatePage and readable by callers.
	PageID  string
	PageURL string

	mu          sync.Mutex
	buffer      []map[string]any
	dedupe      map[string]struct{}
	running     bool
	flushTicker *time.Ticker
	stopFlush   chan struct{}
}

// NewNotionWriter creates a NotionWriter using the supplied config credentials.
func NewNotionWriter(cfg *config.Config) *NotionWriter {
	return &NotionWriter{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		token:      cfg.NotionToken,
		version:    cfg.NotionVersion,
		cfg:        cfg,
		dedupe:     make(map[string]struct{}),
	}
}

// Start begins the background flush loop at cfg.NotionBatchInterval.
// Calling Start on an already-running NotionWriter is a no-op.
func (n *NotionWriter) Start(ctx context.Context) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.running {
		return
	}
	n.running = true
	n.flushTicker = time.NewTicker(n.cfg.NotionBatchInterval)
	n.stopFlush = make(chan struct{})

	go func() {
		for {
			select {
			case <-n.flushTicker.C:
				n.Flush(ctx)
			case <-n.stopFlush:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop halts the flush loop and performs a final flush to drain buffered blocks.
func (n *NotionWriter) Stop(ctx context.Context) {
	n.mu.Lock()
	alreadyStopped := !n.running
	if !alreadyStopped {
		n.running = false
		n.flushTicker.Stop()
		close(n.stopFlush)
	}
	n.mu.Unlock()

	if !alreadyStopped {
		n.Flush(ctx)
	}
}

// CreatePage creates a new Notion page under the configured parent and sets
// PageID / PageURL on the receiver.
func (n *NotionWriter) CreatePage(ctx context.Context, topic string) (pageID, pageURL string, err error) {
	title := FormatNoteTitle(topic)
	body := map[string]any{
		"parent":     map[string]any{"page_id": n.cfg.NotionParentPageID},
		"properties": map[string]any{"title": map[string]any{"title": richText(title)}},
	}
	var resp map[string]any
	if err = n.doJSON(ctx, http.MethodPost, "/pages", body, &resp); err != nil {
		return "", "", fmt.Errorf("notion create page: %w", err)
	}
	id, _ := resp["id"].(string)

	n.mu.Lock()
	n.PageID = id
	n.PageURL = fmt.Sprintf("https://notion.so/%s", strings.ReplaceAll(id, "-", ""))
	n.dedupe = make(map[string]struct{})
	pageID = n.PageID
	pageURL = n.PageURL
	n.mu.Unlock()

	return pageID, pageURL, nil
}

// RenamePage updates the page title to reflect the given topic.
// Returns the new title string, or ("", err) and logs a warning on API error.
func (n *NotionWriter) RenamePage(ctx context.Context, topic string) (string, error) {
	n.mu.Lock()
	pid := n.PageID
	n.mu.Unlock()
	if pid == "" {
		return "", nil
	}
	title := FormatNoteTitle(topic)
	body := map[string]any{
		"properties": map[string]any{"title": map[string]any{"title": richText(title)}},
	}
	if err := n.doJSON(ctx, http.MethodPatch, "/pages/"+pid, body, nil); err != nil {
		log.Warn().Err(err).Msg("notion: page rename failed")
		return "", err
	}
	return title, nil
}

// QueueBlocks deduplicates blocks by SHA-1 of their JSON representation and
// appends the non-duplicate ones to the internal buffer under the mutex.
func (n *NotionWriter) QueueBlocks(blocks []map[string]any) {
	if len(blocks) == 0 {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.PageID == "" {
		return
	}
	for _, block := range blocks {
		key := blockHash(block)
		if _, exists := n.dedupe[key]; exists {
			continue
		}
		n.dedupe[key] = struct{}{}
		n.buffer = append(n.buffer, block)
	}
}

// WriteDecision converts a CuratorDecision into Notion blocks and queues them.
// No blocks are generated when decision.ShouldWrite is false.
func (n *NotionWriter) WriteDecision(ctx context.Context, decision models.CuratorDecision) {
	if !decision.ShouldWrite {
		return
	}
	var blocks []map[string]any

	// heading_2 — concept title
	title := decision.ConceptTitle
	if title == "" {
		title = "Untitled Concept"
	}
	if runes := []rune(title); len(runes) > 100 {
		title = string(runes[:100])
	}
	blocks = append(blocks, map[string]any{
		"type":      "heading_2",
		"heading_2": map[string]any{"rich_text": richText(title)},
	})

	// paragraph — summary (word-wrapped)
	for _, chunk := range splitForNotion(decision.Summary, maxTextLen) {
		blocks = append(blocks, map[string]any{
			"type":      "paragraph",
			"paragraph": map[string]any{"rich_text": richText(chunk)},
		})
	}

	// bulleted_list_item — key points (at most 5, each word-wrapped at 200)
	for i, point := range decision.KeyPoints {
		if i >= 5 {
			break
		}
		for _, chunk := range splitForNotion(point, 200) {
			blocks = append(blocks, map[string]any{
				"type":               "bulleted_list_item",
				"bulleted_list_item": map[string]any{"rich_text": richText(chunk)},
			})
		}
	}

	// callout 💡 — takeaway
	if decision.Takeaway != "" {
		tw := decision.Takeaway
		if runes := []rune(tw); len(runes) > 300 {
			tw = string(runes[:300])
		}
		blocks = append(blocks, map[string]any{
			"type": "callout",
			"callout": map[string]any{
				"icon":      map[string]any{"type": "emoji", "emoji": "💡"},
				"rich_text": richText(tw),
			},
		})
	}

	// divider
	blocks = append(blocks, map[string]any{"type": "divider", "divider": map[string]any{}})

	n.QueueBlocks(blocks)
}

// AppendImageFileUpload queues an image block referencing a Notion file-upload
// ID, then immediately calls Flush so the image appears without waiting for the
// next ticker tick.
func (n *NotionWriter) AppendImageFileUpload(ctx context.Context, fileUploadID, caption string) {
	if fileUploadID == "" {
		return
	}
	cleanCap := cleanImageCaption(caption)
	var captionValue []map[string]any
	if cleanCap != "" {
		captionValue = richText(cleanCap)
	} else {
		captionValue = []map[string]any{}
	}
	imgBlock := map[string]any{
		"type": "image",
		"image": map[string]any{
			"type":        "file_upload",
			"file_upload": map[string]any{"id": fileUploadID},
			"caption":     captionValue,
		},
	}
	n.QueueBlocks([]map[string]any{imgBlock})
	n.Flush(ctx)
}

// Flush drains up to 100 blocks from the buffer and appends them to the current
// Notion page. It logs a warning on API error but does not return an error,
// matching the Python behaviour.
func (n *NotionWriter) Flush(ctx context.Context) {
	n.mu.Lock()
	pid := n.PageID
	if pid == "" || len(n.buffer) == 0 {
		n.mu.Unlock()
		return
	}
	end := len(n.buffer)
	if end > 100 {
		end = 100
	}
	chunk := make([]map[string]any, end)
	copy(chunk, n.buffer[:end])
	n.buffer = n.buffer[end:]
	n.mu.Unlock()

	body := map[string]any{"children": chunk}
	if err := n.doJSON(ctx, http.MethodPatch, "/blocks/"+pid+"/children", body, nil); err != nil {
		log.Warn().Err(err).Int("blocks", len(chunk)).Msg("notion: append blocks failed")
	}
}

// ── internal HTTP helper ──────────────────────────────────────────────────────

func (n *NotionWriter) doJSON(ctx context.Context, method, path string, body, out any) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("notion: marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, notionAPIBase+path, bodyReader)
	if err != nil {
		return fmt.Errorf("notion: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+n.token)
	req.Header.Set("Notion-Version", n.version)
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("notion: http %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("notion: api error %d %s %s: %s", resp.StatusCode, method, path, b)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
