package agenttools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

func readOnlyTools(opts Options) []Tool {
	return []Tool{
		listDirTool(opts),
		readFileTool(opts),
		readFileRangeTool(opts),
		searchFilesTool(opts),
		grepTool(opts),
		gitStatusTool(opts),
		gitDiffTool(opts),
		reportChangedFilesTool(opts),
	}
}

type listDirArgs struct {
	Path string `json:"path"`
}

func listDirTool(opts Options) Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Directory path relative to the project root (default \".\")."}},"required":[]}`)
	return &funcTool{
		name:        "list_dir",
		description: "List the entries of a directory within the project (name, kind, size).",
		schema:      schema,
		exec: func(ctx context.Context, raw json.RawMessage) Result {
			var a listDirArgs
			if err := parseArgs(raw, &a); err != nil {
				return errResult("invalid arguments: %v", err)
			}
			if a.Path == "" {
				a.Path = "."
			}
			resolved, err := opts.Workspace.Resolve(a.Path)
			if err != nil {
				return errResult("%v", err)
			}
			entries, err := os.ReadDir(resolved)
			if err != nil {
				return errResult("list directory: %v", err)
			}
			const maxEntries = 500
			truncated := len(entries) > maxEntries
			if truncated {
				entries = entries[:maxEntries]
			}
			var b strings.Builder
			for _, e := range entries {
				kind := "file"
				var size int64
				if e.IsDir() {
					kind = "dir"
				} else if info, ierr := e.Info(); ierr == nil {
					size = info.Size()
				}
				fmt.Fprintf(&b, "%s\t%s\t%d\n", kind, e.Name(), size)
			}
			if b.Len() == 0 {
				return Result{Content: "(empty directory)"}
			}
			if truncated {
				b.WriteString("... (truncated, more entries exist)\n")
			}
			return Result{Content: b.String()}
		},
	}
}

type readFileArgs struct {
	Path string `json:"path"`
}

func readFileTool(opts Options) Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path relative to the project root."}},"required":["path"]}`)
	return &funcTool{
		name:        "read_file",
		description: "Read a text file's contents, capped at a maximum size.",
		schema:      schema,
		exec: func(ctx context.Context, raw json.RawMessage) Result {
			var a readFileArgs
			if err := parseArgs(raw, &a); err != nil {
				return errResult("invalid arguments: %v", err)
			}
			if strings.TrimSpace(a.Path) == "" {
				return errResult("path is required")
			}
			resolved, err := opts.Workspace.Resolve(a.Path)
			if err != nil {
				return errResult("%v", err)
			}
			info, err := os.Stat(resolved)
			if err != nil {
				return errResult("read file: %v", err)
			}
			if info.IsDir() {
				return errResult("path is a directory: %s", a.Path)
			}
			f, err := os.Open(resolved)
			if err != nil {
				return errResult("read file: %v", err)
			}
			defer f.Close()
			limit := opts.MaxReadBytes
			buf := make([]byte, limit+1)
			n, err := io.ReadFull(f, buf)
			if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
				return errResult("read file: %v", err)
			}
			data := buf[:n]
			truncated := n > limit
			if truncated {
				data = data[:limit]
			}
			if looksBinary(data) {
				return errResult("refusing to read binary file as text: %s", a.Path)
			}
			content := string(data)
			if truncated {
				content += "\n... (truncated, file exceeds max read size)"
			}
			return Result{Content: content}
		},
	}
}

type readFileRangeArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

func readFileRangeTool(opts Options) Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path relative to the project root."},"start_line":{"type":"integer","description":"1-based inclusive start line."},"end_line":{"type":"integer","description":"1-based inclusive end line."}},"required":["path","start_line","end_line"]}`)
	return &funcTool{
		name:        "read_file_range",
		description: "Read an inclusive 1-based line range from a text file.",
		schema:      schema,
		exec: func(ctx context.Context, raw json.RawMessage) Result {
			var a readFileRangeArgs
			if err := parseArgs(raw, &a); err != nil {
				return errResult("invalid arguments: %v", err)
			}
			if strings.TrimSpace(a.Path) == "" {
				return errResult("path is required")
			}
			if a.StartLine < 1 || a.EndLine < a.StartLine {
				return errResult("invalid line range: start_line=%d end_line=%d", a.StartLine, a.EndLine)
			}
			resolved, err := opts.Workspace.Resolve(a.Path)
			if err != nil {
				return errResult("%v", err)
			}
			f, err := os.Open(resolved)
			if err != nil {
				return errResult("read file: %v", err)
			}
			defer f.Close()
			probe := make([]byte, 8192)
			pn, _ := f.Read(probe)
			if looksBinary(probe[:pn]) {
				return errResult("refusing to read binary file as text: %s", a.Path)
			}
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return errResult("read file: %v", err)
			}
			scanner := bufio.NewScanner(f)
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			var b strings.Builder
			line := 0
			budget := opts.MaxReadBytes
			truncated := false
			for scanner.Scan() {
				line++
				if line < a.StartLine {
					continue
				}
				if line > a.EndLine {
					break
				}
				text := fmt.Sprintf("%d: %s\n", line, scanner.Text())
				if len(text) > budget {
					truncated = true
					break
				}
				budget -= len(text)
				b.WriteString(text)
			}
			if err := scanner.Err(); err != nil {
				return errResult("read file: %v", err)
			}
			if b.Len() == 0 {
				return Result{Content: "(no lines in requested range)"}
			}
			if truncated {
				b.WriteString("... (truncated)\n")
			}
			return Result{Content: b.String()}
		},
	}
}

type searchFilesArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

func searchFilesTool(opts Options) Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Glob pattern, e.g. \"*.go\"."},"path":{"type":"string","description":"Directory to search within (default \".\")."}},"required":["pattern"]}`)
	return &funcTool{
		name:        "search_files",
		description: "Find files whose name or relative path matches a glob pattern.",
		schema:      schema,
		exec: func(ctx context.Context, raw json.RawMessage) Result {
			var a searchFilesArgs
			if err := parseArgs(raw, &a); err != nil {
				return errResult("invalid arguments: %v", err)
			}
			if a.Pattern == "" {
				return errResult("pattern is required")
			}
			if a.Path == "" {
				a.Path = "."
			}
			resolvedBase, err := opts.Workspace.Resolve(a.Path)
			if err != nil {
				return errResult("%v", err)
			}
			const maxResults = 500
			var matches []string
			walkErr := filepath.WalkDir(resolvedBase, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if d.IsDir() {
					return nil
				}
				rel, rerr := filepath.Rel(opts.Workspace.Root(), p)
				if rerr != nil {
					return nil
				}
				rel = filepath.ToSlash(rel)
				matched, merr := path.Match(a.Pattern, path.Base(rel))
				if merr != nil {
					return merr
				}
				if !matched {
					matched, merr = path.Match(a.Pattern, rel)
					if merr != nil {
						return merr
					}
				}
				if matched {
					matches = append(matches, rel)
					if len(matches) >= maxResults {
						return errStopWalk
					}
				}
				return nil
			})
			if walkErr != nil && walkErr != errStopWalk {
				return errResult("search files: %v", walkErr)
			}
			if len(matches) == 0 {
				return Result{Content: "(no matches)"}
			}
			out := strings.Join(matches, "\n")
			if len(matches) >= maxResults {
				out += "\n... (truncated, more matches exist)"
			}
			return Result{Content: out}
		},
	}
}

type grepArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Glob    string `json:"glob"`
}

func grepTool(opts Options) Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Regular expression to search for."},"path":{"type":"string","description":"Directory to search within (default \".\")."},"glob":{"type":"string","description":"Optional glob filter applied to file names/paths."}},"required":["pattern"]}`)
	return &funcTool{
		name:        "grep",
		description: "Search text files for lines matching a regular expression.",
		schema:      schema,
		exec: func(ctx context.Context, raw json.RawMessage) Result {
			var a grepArgs
			if err := parseArgs(raw, &a); err != nil {
				return errResult("invalid arguments: %v", err)
			}
			if a.Pattern == "" {
				return errResult("pattern is required")
			}
			re, err := regexp.Compile(a.Pattern)
			if err != nil {
				return errResult("invalid pattern: %v", err)
			}
			if a.Path == "" {
				a.Path = "."
			}
			resolvedBase, err := opts.Workspace.Resolve(a.Path)
			if err != nil {
				return errResult("%v", err)
			}
			const maxMatches = 200
			var b strings.Builder
			count := 0
			walkErr := filepath.WalkDir(resolvedBase, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if d.IsDir() {
					return nil
				}
				rel, rerr := filepath.Rel(opts.Workspace.Root(), p)
				if rerr != nil {
					return nil
				}
				rel = filepath.ToSlash(rel)
				if a.Glob != "" {
					matched, _ := path.Match(a.Glob, path.Base(rel))
					if !matched {
						matched, _ = path.Match(a.Glob, rel)
					}
					if !matched {
						return nil
					}
				}
				f, ferr := os.Open(p)
				if ferr != nil {
					return nil
				}
				defer f.Close()
				probe := make([]byte, 8192)
				pn, _ := f.Read(probe)
				if looksBinary(probe[:pn]) {
					return nil
				}
				if _, serr := f.Seek(0, io.SeekStart); serr != nil {
					return nil
				}
				scanner := bufio.NewScanner(f)
				scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
				line := 0
				for scanner.Scan() {
					line++
					text := scanner.Text()
					if re.MatchString(text) {
						fmt.Fprintf(&b, "%s:%d: %s\n", rel, line, text)
						count++
						if count >= maxMatches {
							return errStopWalk
						}
					}
				}
				if err := scanner.Err(); err != nil {
					return err
				}
				return nil
			})
			if walkErr != nil && walkErr != errStopWalk {
				return errResult("grep: %v", walkErr)
			}
			if b.Len() == 0 {
				return Result{Content: "(no matches)"}
			}
			if count >= maxMatches {
				b.WriteString("... (truncated, more matches exist)\n")
			}
			return Result{Content: b.String()}
		},
	}
}
