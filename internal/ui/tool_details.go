package ui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/app"
)

func toolCaption(t *app.Tool) string {
	key := [5]string{t.Kind, t.Name, t.Command, t.Input, t.ParentToolID}
	if t.DisplayCache.CaptionKey == key && t.DisplayCache.Caption != "" {
		return t.DisplayCache.Caption
	}
	op := computeToolOperationCaption(t)
	if caption := op; caption != "" {
		if t.ParentToolID != "" {
			caption = "子任务 · " + caption
		}
		t.DisplayCache.CaptionKey, t.DisplayCache.OperationCaption, t.DisplayCache.Caption = key, op, caption
		return caption
	}
	caption := ""
	var args struct {
		Title string `json:"title"`
	}
	if json.Unmarshal([]byte(t.Input), &args) == nil && strings.TrimSpace(args.Title) != "" {
		caption = strings.Join(strings.Fields(args.Title), " ")
	} else if t.Name != "" {
		caption = t.Name
	} else {
		caption = t.Title()
	}
	if t.ParentToolID != "" {
		caption = "子任务 · " + caption
	}
	t.DisplayCache.CaptionKey, t.DisplayCache.OperationCaption, t.DisplayCache.Caption = key, op, caption
	return caption
}

func toolOperationCaption(tool *app.Tool) string {
	_ = toolCaption(tool)
	return tool.DisplayCache.OperationCaption
}

func computeToolOperationCaption(tool *app.Tool) string {
	name := tool.Name
	if tool.Kind == "commandExecution" {
		if name == "" {
			name = "Bash"
		}
		if tool.Command != "" {
			return name + " " + strconv.Quote(strings.Join(strings.Fields(tool.Command), " "))
		}
	}
	var keys []string
	switch strings.ToLower(name) {
	case "read", "edit", "write":
		keys = []string{"file_path", "path", "filePath"}
	case "bash":
		keys = []string{"command"}
	case "toolsearch", "websearch", "search":
		keys = []string{"query", "search_term"}
	case "grep":
		keys = []string{"pattern"}
	case "glob":
		keys = []string{"pattern"}
	case "webfetch":
		keys = []string{"url"}
	default:
		return ""
	}
	var input map[string]json.RawMessage
	if json.Unmarshal([]byte(tool.Input), &input) == nil {
		for _, key := range keys {
			var value string
			if json.Unmarshal(input[key], &value) == nil && strings.TrimSpace(value) != "" {
				return name + " " + strconv.Quote(value)
			}
		}
	}
	return name
}

func toolResultFailed(tool *app.Tool) bool {
	if tool.Kind != "mcpToolCall" && tool.Kind != "dynamicToolCall" {
		return false
	}
	ensureToolOutputCache(tool)
	return tool.DisplayCache.ResultFailed
}

func ensureToolInputCache(tool *app.Tool) (code, parameters string) {
	if tool.DisplayCache.InputParsed && tool.DisplayCache.InputRaw == tool.Input {
		return tool.DisplayCache.Code, tool.DisplayCache.Parameters
	}
	code, parameters = toolInput(tool.Input)
	tool.DisplayCache.InputRaw = tool.Input
	tool.DisplayCache.InputParsed = true
	tool.DisplayCache.Code = code
	tool.DisplayCache.Parameters = parameters
	return code, parameters
}

func ensureToolOutputCache(tool *app.Tool) {
	if tool.DisplayCache.OutputParsed && tool.DisplayCache.OutputRaw == tool.Output && tool.DisplayCache.OutputKind == tool.Kind {
		return
	}
	var result struct {
		IsError bool `json:"isError"`
	}
	failed := (tool.Kind == "mcpToolCall" || tool.Kind == "dynamicToolCall") &&
		json.Unmarshal([]byte(tool.Output), &result) == nil && result.IsError
	tool.DisplayCache.OutputRaw = tool.Output
	tool.DisplayCache.OutputKind = tool.Kind
	tool.DisplayCache.OutputParsed = true
	tool.DisplayCache.OutputFormatted = false
	tool.DisplayCache.ResultFailed = failed
	tool.DisplayCache.FormattedOutput = ""
	tool.DisplayCache.ErrorSummary = ""
}

// Collapsed successful records need only their status, not a formatted copy of
// potentially large output. Allocate that copy when details or an error show.
func ensureToolFormattedOutputCache(tool *app.Tool) {
	ensureToolOutputCache(tool)
	if tool.DisplayCache.OutputFormatted {
		return
	}
	if tool.Output != "" {
		tool.DisplayCache.FormattedOutput = toolResult(tool.Output, 0)
		if tool.DisplayCache.ResultFailed {
			tool.DisplayCache.ErrorSummary = strings.Join(strings.Fields(tool.DisplayCache.FormattedOutput), " ")
		}
	}
	tool.DisplayCache.OutputFormatted = true
}

// Only unwrap recognized text envelopes. Unknown fields and non-text blocks
// remain visible as JSON, and malformed/truncated input retains its exact text.
func toolResult(raw string, depth int) string {
	if depth >= 16 {
		return raw
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if !json.Valid([]byte(raw)) || decoder.Decode(&value) != nil {
		return raw
	}
	switch v := value.(type) {
	case string:
		return toolResult(v, depth+1)
	case map[string]any:
		if content, ok := v["content"].([]any); ok && len(content) > 0 {
			var parts []string
			if v["isError"] == true {
				parts = append(parts, "工具返回错误")
			}
			for _, block := range content {
				data, _ := json.Marshal(block)
				parts = append(parts, toolResult(string(data), depth+1))
			}
			delete(v, "content")
			delete(v, "isError")
			if len(v) > 0 {
				metadata, _ := json.Marshal(v)
				parts = append(parts, pretty(metadata))
			}
			return strings.Join(parts, "\n\n")
		}
		if (v["type"] == "text" || v["type"] == "input_text") && len(v) == 2 {
			if text, ok := v["text"].(string); ok {
				return toolResult(text, depth+1)
			}
		}
	}
	return pretty(json.RawMessage(raw))
}

func toolInput(raw string) (code, parameters string) {
	var args map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &args) == nil && args != nil {
		if json.Unmarshal(args["code"], &code) == nil && code != "" {
			delete(args, "code")
			// The title is already the disclosure caption.
			delete(args, "title")
			if len(args) > 0 {
				rest, _ := json.Marshal(args)
				parameters = pretty(rest)
			}
			return code, parameters
		}
	}
	return "", pretty(json.RawMessage(raw))
}

var toolIcon = icon(`<rect x="3" y="3" width="7" height="7" rx="1.3"/><rect x="14" y="3" width="7" height="7" rx="1.3"/><rect x="3" y="14" width="7" height="7" rx="1.3"/><rect x="14" y="14" width="7" height="7" rx="1.3"/>`)

func (a *App) toolDisclosure(c *native.Context, tool *app.Tool) {
	a.toolDisclosureWithIcon(c, tool, toolIcon)
}

func (a *App) toolDisclosureWithIcon(c *native.Context, tool *app.Tool, glyph *native.SVG) {
	theme := c.Theme()
	failed := commandFailed(tool)
	native.Column(c).FillWidth().MinWidth(0).Children(func() {
		a.processDisclosure(c, "工具记录 "+tool.Name, toolCaption(tool), &tool.Expanded, glyph, tool.Status == "inProgress", func() {
			if tool.Status == "inProgress" {
				native.Spinner(c).Size(12, 12).Shrink(0)
				native.Text(c, tool.StatusLabel()).FontSize(11).SingleLine().Shrink(0).TextColor(theme.TextMuted)
			} else if failed || tool.Status != "completed" {
				label := tool.StatusLabel()
				if failed {
					label = "执行失败"
				}
				native.Text(c, label).FontSize(11).SingleLine().Shrink(0).TextColor(theme.TextMuted)
			}
			if tool.DurationMs != nil {
				native.Text(c, fmt.Sprintf("%.2f 秒", float64(*tool.DurationMs)/1000)).FontSize(11).SingleLine().Shrink(0).TextColor(theme.TextMuted)
			}
		}, a.toolDetailsContent(c, tool))
		if failed && tool.Output != "" && !tool.Expanded {
			ensureToolFormattedOutputCache(tool)
			summary := tool.DisplayCache.ErrorSummary
			if summary == "" {
				summary = strings.Join(strings.Fields(tool.DisplayCache.FormattedOutput), " ")
			}
			native.Text(c, summary).Label("工具错误摘要").FontSize(12).TextColor(theme.TextMuted).FillWidth().MaxLines(2).Padding(0, 0, 4, 24)
		}
		if tool.Truncated && !tool.Expanded {
			native.Text(c, "内容显示已截断").FontSize(11).TextColor(theme.TextMuted).Padding(0, 0, 4, 24)
		}
	})
}

func (a *App) toolDetailsContent(c *native.Context, tool *app.Tool) func() {
	if strings.TrimSpace(tool.Input) == "" && strings.TrimSpace(tool.Output) == "" && !tool.Truncated {
		return nil
	}
	theme := c.Theme()
	return func() {
		native.Column(c).FillWidth().MinWidth(0).Gap(10).Padding(10, 12).Radius(8).
			Border(1, theme.Border.Alpha(.5)).Background(theme.Surface).Children(func() {
			if tool.Name != "" {
				native.Text(c, tool.Name).FontSize(11).TextColor(theme.TextMuted).SingleLine()
			}
			if tool.Input != "" {
				code, parameters := ensureToolInputCache(tool)
				if code != "" {
					toolDetailSection(c.Key("code"), "输入", code, true, a.codeFontFamily())
				}
				if parameters != "" {
					title := "输入"
					if code != "" {
						title = "参数"
					}
					toolDetailSection(c.Key("input"), title, parameters, true, a.codeFontFamily())
				}
			}
			if tool.Output != "" {
				ensureToolFormattedOutputCache(tool)
				toolDetailSection(c.Key("output"), "结果", tool.DisplayCache.FormattedOutput, false)
			}
			if tool.Truncated {
				native.Text(c, "内容显示已截断（上限 256 KiB），请核验完整运行记录。").FontSize(11).TextColor(theme.TextMuted).FillWidth()
			}
		})
	}
}

func toolDetailSection(c *native.Context, title, text string, code bool, fonts ...string) {
	family := codeFont
	if len(fonts) > 0 {
		family = fonts[0]
	}
	native.Column(c).FillWidth().MinWidth(0).Gap(4).TextColor(c.Theme().TextMuted).Children(func() {
		native.Row(c).FillWidth().AlignItems(native.Center).Children(func() {
			native.Text(c, title).FontSize(11).TextColor(c.Theme().TextMuted).Grow(1)
			b := quietIconButton(c, "复制"+title).Size(24, 24)
			b.Children(func() { native.Icon(c, copyIcon).Size(13, 13) })
			if b.Clicked() {
				c.WriteClipboard(text)
			}
		})
		native.Scroll(c.Key("vertical")).Label("工具" + title).FillWidth().MaxHeight(280).Children(func() {
			if code {
				native.ScrollHorizontal(c.Key("horizontal")).FillWidth().Children(func() {
					native.Text(c, text).Selectable().Font(family).FontSize(12).TextColor(c.Theme().TextMuted).NoWrap()
				})
			} else {
				native.Text(c, text).Selectable().FontSize(12).TextColor(c.Theme().TextMuted).FillWidth()
			}
		})
	})
}
