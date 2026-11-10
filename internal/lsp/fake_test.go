package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// The fake language server is this test binary re-run as
// `<test binary> fake-lsp-server <mode,...>` (see TestMain). It speaks the
// framing over its stdio and keeps the documents it is sent, so tests can ask
// it what it saw through the extra request "fake/state".
//
// Modes (comma-separated):
//
//	array      completion answers a plain CompletionItem[] (default: a CompletionList)
//	incomplete the CompletionList has isIncomplete=true
//	textedit   items carry a textEdit over the word before the cursor
//	snippet    the item is a snippet (insertTextFormat 2)
//	slow       completion never answers until cancelled
//	crash      the process exits 3 on a completion request
//	crashstart the process exits 3 before reading anything
//	noexit     the exit notification is ignored (and stdin EOF too)
//	nocomplete initialize advertises no completionProvider
//	ask        before answering initialize, the server asks the client
//	           workspace/configuration and window/workDoneProgress/create
//	           and an unknown method, and fails initialize if any goes unanswered
//	slowinit   initialize is never answered
//	child      at start the server runs a child of its own (`fake-lsp-sleeper`,
//	           sleeps an hour); its pid is fakeState.Child
//	garbage    a completion request is answered with a broken header line
//	pidfile=P  at start the server writes its pid to file P

const (
	fakeArg     = "fake-lsp-server"
	sleeperArg  = "fake-lsp-sleeper"
	pidfileMode = "pidfile="
)

func TestMain(m *testing.M) {
	if len(os.Args) >= 3 && os.Args[1] == fakeArg {
		os.Exit(runFakeServer(strings.Split(os.Args[2], ",")))
	}
	if len(os.Args) >= 2 && os.Args[1] == sleeperArg {
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeServer is the Server value that runs the fake in mode.
func fakeServer(t *testing.T, modes ...string) Server {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	mode := strings.Join(modes, ",")
	if mode == "" {
		mode = "list"
	}
	return Server{Lang: "go", Name: "fake", Command: []string{exe, fakeArg, mode}}
}

// fakeState is the fake's answer to "fake/state".
type fakeState struct {
	Docs        map[string]fakeDoc `json:"docs"`
	Methods     []string           `json:"methods"` // every method received, in order
	Init        json.RawMessage    `json:"init"`    // initialize params
	Answers     map[string]string  `json:"answers"` // answers to the server's own requests
	Env         map[string]string  `json:"env"`
	Cancelled   []int64            `json:"cancelled"`
	Initialized bool               `json:"initialized"`
	Child       int                `json:"child"` // pid of the "child" mode's own child
}

type fakeDoc struct {
	Version int    `json:"version"`
	Text    string `json:"text"`
	Lang    string `json:"lang"`
}

func runFakeServer(modes []string) int {
	has := func(m string) bool { return slices.Contains(modes, m) }
	if has("crashstart") {
		fmt.Fprintln(os.Stderr, "fake: crashing at start")
		return 3
	}
	r := bufio.NewReader(os.Stdin)
	var wmu sync.Mutex
	send := func(v any) {
		b, _ := encodeFrame(v)
		wmu.Lock()
		os.Stdout.Write(b)
		wmu.Unlock()
	}
	st := fakeState{Docs: map[string]fakeDoc{}, Answers: map[string]string{}, Env: map[string]string{}}
	for _, m := range modes {
		if p, ok := strings.CutPrefix(m, pidfileMode); ok {
			_ = os.WriteFile(p, []byte(strconv.Itoa(os.Getpid())), 0o600)
		}
	}
	if has("child") {
		exe, _ := os.Executable()
		child := exec.Command(exe, sleeperArg) // stdio: the null device
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "fake: child:", err)
			return 4
		}
		st.Child = child.Process.Pid
	}
	for _, k := range []string{"LEETCODE_SESSION", "LEETCODE_CSRFTOKEN", "LEETCODE_CFCLEARANCE", "FAKE_LSP_KEEP"} {
		st.Env[k] = os.Getenv(k)
	}
	var mu sync.Mutex
	shutdown := false
	pendingInit := json.RawMessage(nil)
	asked := 0
	fmt.Fprintln(os.Stderr, "fake: started")

	answerInit := func(id json.RawMessage) {
		caps := map[string]any{"textDocumentSync": 1}
		if !has("nocomplete") {
			caps["completionProvider"] = map[string]any{"triggerCharacters": []string{"."}}
		}
		send(outResponse{JSONRPC: "2.0", ID: id, Result: map[string]any{"capabilities": caps}})
	}

	for {
		m, err := readMessage(r)
		if err != nil {
			if has("noexit") {
				time.Sleep(time.Hour)
			}
			return 0
		}
		mu.Lock()
		if m.Method != "" {
			st.Methods = append(st.Methods, m.Method)
		}
		mu.Unlock()
		switch m.Method {
		case "initialize":
			st.Init = m.Params
			if has("slowinit") {
				continue
			}
			if has("ask") {
				pendingInit = m.ID
				send(map[string]any{"jsonrpc": "2.0", "id": "c1", "method": "workspace/configuration",
					"params": map[string]any{"items": []any{map[string]any{"section": "a"}, map[string]any{"section": "b"}}}})
				send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "window/workDoneProgress/create",
					"params": map[string]any{"token": "t"}})
				send(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "fake/unknown"})
				send(map[string]any{"jsonrpc": "2.0", "method": "window/logMessage",
					"params": map[string]any{"type": 3, "message": "hello"}})
				continue
			}
			answerInit(m.ID)
		case "initialized":
			st.Initialized = true
		case "textDocument/didOpen":
			var p struct {
				TextDocument struct {
					URI, LanguageID, Text string
					Version               int
				} `json:"textDocument"`
			}
			json.Unmarshal(m.Params, &p)
			mu.Lock()
			st.Docs[p.TextDocument.URI] = fakeDoc{Version: p.TextDocument.Version, Text: p.TextDocument.Text, Lang: p.TextDocument.LanguageID}
			mu.Unlock()
		case "textDocument/didChange":
			var p struct {
				TextDocument   versionedDocID `json:"textDocument"`
				ContentChanges []struct {
					Text string `json:"text"`
				} `json:"contentChanges"`
			}
			json.Unmarshal(m.Params, &p)
			mu.Lock()
			d := st.Docs[p.TextDocument.URI]
			d.Version = p.TextDocument.Version
			if n := len(p.ContentChanges); n > 0 {
				d.Text = p.ContentChanges[n-1].Text
			}
			st.Docs[p.TextDocument.URI] = d
			mu.Unlock()
		case "textDocument/didClose":
			var p struct {
				TextDocument textDocumentID `json:"textDocument"`
			}
			json.Unmarshal(m.Params, &p)
			mu.Lock()
			delete(st.Docs, p.TextDocument.URI)
			mu.Unlock()
		case "$/cancelRequest":
			var p struct {
				ID int64 `json:"id"`
			}
			json.Unmarshal(m.Params, &p)
			mu.Lock()
			st.Cancelled = append(st.Cancelled, p.ID)
			mu.Unlock()
			send(outResponse{JSONRPC: "2.0", ID: json.RawMessage(fmt.Sprint(p.ID)), Error: &rpcError{Code: codeRequestCancelled, Message: "cancelled"}})
		case "textDocument/completion":
			if has("crash") {
				fmt.Fprintln(os.Stderr, "fake: crashing on completion")
				os.Exit(3)
			}
			if has("slow") {
				continue // the cancel answers it
			}
			if has("garbage") {
				wmu.Lock()
				os.Stdout.WriteString("garbage\r\n\r\n")
				wmu.Unlock()
				continue
			}
			var p struct {
				TextDocument textDocumentID `json:"textDocument"`
				Position     Position       `json:"position"`
			}
			json.Unmarshal(m.Params, &p)
			mu.Lock()
			text := st.Docs[p.TextDocument.URI].Text
			mu.Unlock()
			send(outResponse{JSONRPC: "2.0", ID: m.ID, Result: fakeCompletion(has, text, p.Position)})
		case "fake/state":
			mu.Lock()
			b, _ := json.Marshal(st)
			mu.Unlock()
			send(outResponse{JSONRPC: "2.0", ID: m.ID, Result: json.RawMessage(b)})
		case "shutdown":
			shutdown = true
			send(outResponse{JSONRPC: "2.0", ID: m.ID, Result: nil})
		case "exit":
			if has("noexit") {
				continue
			}
			if shutdown {
				return 0
			}
			return 1
		case "":
			// a response to one of the "ask" requests
			if pendingInit != nil {
				id := strings.Trim(string(m.ID), `"`)
				switch {
				case m.Error != nil:
					st.Answers[id] = fmt.Sprintf("error %d", m.Error.Code)
				default:
					st.Answers[id] = string(m.Result)
				}
				asked++
				if asked == 3 {
					answerInit(pendingInit)
					pendingInit = nil
				}
			}
		default:
			if !isNull(m.ID) {
				send(outResponse{JSONRPC: "2.0", ID: m.ID, Error: &rpcError{Code: codeMethodNotFound, Message: m.Method}})
			}
		}
	}
}

// fakeCompletion answers with the word before the cursor echoed in Detail and
// two items, Println and Printf (sortText puts Println first), plus one
// unlabeled item that must be dropped.
func fakeCompletion(has func(string) bool, text string, pos Position) any {
	lines := strings.Split(text, "\n")
	line := ""
	if pos.Line < len(lines) {
		line = lines[pos.Line]
	}
	runeCol := RuneCol(line, pos.Character)
	rs := []rune(line)
	start := runeCol
	for start > 0 && (isWordRune(rs[start-1]) || isDigit(rs[start-1]) || rs[start-1] >= utf8.RuneSelf) {
		start--
	}
	word := string(rs[start:runeCol])
	mk := func(label, sort string) map[string]any {
		it := map[string]any{"label": label, "kind": int(KindFunction), "detail": "prefix=" + word, "sortText": sort}
		if has("textedit") {
			it["textEdit"] = map[string]any{
				"range":   Range{Start: Position{pos.Line, UTF16Col(line, start)}, End: pos},
				"newText": label,
			}
		}
		if has("snippet") {
			it["insertText"] = label + "(${1:a}, $2)$0"
			it["insertTextFormat"] = 2
		}
		return it
	}
	items := []any{mk("Printf", "b"), mk("Println", "a"), map[string]any{"label": ""}}
	if has("array") {
		return items
	}
	return map[string]any{"isIncomplete": has("incomplete"), "items": items}
}
