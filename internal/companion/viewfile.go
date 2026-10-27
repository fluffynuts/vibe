package companion

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unicode/utf8"

	"vibe/internal/review"
)

// maxViewedFile is the most of a file the page is given to show.
const maxViewedFile = 1 << 20

// maxAskedPath is the longest path taken: a real one is far shorter.
const maxAskedPath = 500

// fileRefused is why a file can't be shown, in the user's terms. Kind is for
// the page to tell the cases apart: bad, outside, missing, notFile, tooBig,
// notText.
type fileRefused struct {
	Kind    string
	Message string
}

func (f fileRefused) Error() string { return f.Message }

var refusalStatus = map[string]int{
	"bad": http.StatusBadRequest, "outside": http.StatusForbidden, "missing": http.StatusNotFound,
	"notFile": http.StatusUnprocessableEntity, "tooBig": http.StatusRequestEntityTooLarge, "notText": http.StatusUnsupportedMediaType,
}

// workspaceFile is a file of the workspace, as the page is given it.
type workspaceFile struct {
	// Path is where it is, from the workspace, with forward slashes.
	Path string `json:"path"`
	Text string `json:"text"`
	// Colour is the text of a code file as coloured runs, a line at a time, as
	// the Review tab has them, or why it isn't (too long); nil for Markdown,
	// which the page draws as Markdown, and for text whose language isn't
	// known.
	Colour *review.Highlighted `json:"colour,omitempty"`
}

// coloured is file with its text coloured as its language says, where it
// has one. The page shows text with CRLF line ends as LF and checks each of
// its lines against the colours of it, so what is coloured is that text.
func coloured(file workspaceFile) workspaceFile {
	if ext := strings.ToLower(filepath.Ext(file.Path)); ext == ".md" || ext == ".markdown" {
		return file
	}
	if got := review.Highlight(file.Path, strings.ReplaceAll(file.Text, "\r\n", "\n")); len(got.Lines) > 0 || got.Skipped != "" {
		file.Colour = &got
	}
	return file
}

// openWorkspaceFile opens asked, a path as the agent wrote it (from the
// workspace, or whole), if it is a plain file in the workspace: the agent
// chooses the path, so nothing outside the workspace is opened, whether it's
// named by .. or by a link the agent made, and nothing but a plain file. A
// folder's own real path counts as it, as the agent may name that.
func openWorkspaceFile(workspace, asked string) (*os.File, string, error) {
	if asked == "" || len(asked) > maxAskedPath || strings.ContainsRune(asked, 0) {
		return nil, "", fileRefused{"bad", "that isn't a path to a file"}
	}
	outside := fileRefused{"outside", fmt.Sprintf("%s is outside the folder this sandbox works in, so vibe won't show it", asked)}
	if strings.HasPrefix(asked, "~") {
		return nil, "", outside
	}
	rel, ok := fromWorkspace(workspace, filepath.FromSlash(asked))
	if !ok {
		return nil, "", outside
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, "", fmt.Errorf("opening the workspace: %w", err)
	}
	defer root.Close()
	// Not blocking: a pipe a sandbox made would otherwise hold the open.
	flags := os.O_RDONLY
	if runtime.GOOS != "windows" {
		flags |= syscall.O_NONBLOCK
	}
	f, err := root.OpenFile(rel, flags, 0)
	switch {
	case err == nil:
	case os.IsNotExist(err):
		return nil, "", fileRefused{"missing", fmt.Sprintf("there is no file %s in the workspace", asked)}
	case strings.Contains(err.Error(), "escapes from parent"):
		return nil, "", outside
	default:
		return nil, "", fileRefused{"notFile", fmt.Sprintf("%s can't be opened: %v", asked, errors.Unwrap(err))}
	}
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, "", fileRefused{"notFile", fmt.Sprintf("%s isn't a file", asked)}
	}
	return f, rel, nil
}

// readWorkspaceFile reads asked if it is a text file in the workspace (see
// openWorkspaceFile).
func readWorkspaceFile(workspace, asked string) (workspaceFile, error) {
	f, rel, err := openWorkspaceFile(workspace, asked)
	if err != nil {
		return workspaceFile{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxViewedFile+1))
	if err != nil {
		return workspaceFile{}, fmt.Errorf("reading %s: %w", asked, err)
	}
	if len(data) > maxViewedFile {
		return workspaceFile{}, fileRefused{"tooBig", fmt.Sprintf("%s is bigger than the %d MB vibe shows", asked, maxViewedFile>>20)}
	}
	if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return workspaceFile{}, fileRefused{"notText", fmt.Sprintf("%s doesn't look like a text file", asked)}
	}
	return workspaceFile{Path: filepath.ToSlash(rel), Text: string(data)}, nil
}

// maxCheckedPaths is the most paths looked at in one ask.
const maxCheckedPaths = 50

// sniffed is how much of a file is looked at to tell whether it is text.
const sniffed = 8 << 10

// viewableFiles are those of asked (the first maxCheckedPaths) that are files
// the page could show: so it offers a button only for what is there, and not
// for every bit of code that reads like a path. Only the start of each is
// looked at, so this is cheap, and a file that turns out not to be text
// when read whole is still refused then.
func viewableFiles(workspace string, asked []string) []string {
	if len(asked) > maxCheckedPaths {
		asked = asked[:maxCheckedPaths]
	}
	var out []string
	for _, path := range asked {
		f, _, err := openWorkspaceFile(workspace, path)
		if err != nil {
			continue
		}
		head := make([]byte, sniffed)
		n, _ := io.ReadFull(f, head)
		info, statErr := f.Stat()
		f.Close()
		if statErr == nil && info.Size() <= maxViewedFile && !strings.ContainsRune(string(head[:n]), 0) {
			out = append(out, path)
		}
	}
	return out
}

// fromWorkspace is path from the workspace, if that is where it is: it is
// either already from there, or whole and under the workspace (by the name
// it has or its real one).
func fromWorkspace(workspace, path string) (string, bool) {
	if filepath.IsAbs(path) {
		roots := []string{workspace}
		if real, err := filepath.EvalSymlinks(workspace); err == nil {
			roots = append(roots, real)
		}
		for _, root := range roots {
			if rel, err := filepath.Rel(root, path); err == nil && stays(rel) {
				return rel, true
			}
		}
		return "", false
	}
	rel := filepath.Clean(path)
	return rel, stays(rel)
}

func stays(rel string) bool {
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

var fileFeature = feature{func(s *Server) bool { return s.cfg.Workspace != "" },
	"this sandbox has no folder to show files from", 1 << 10}

// workspaceFileHandler gives the text of a file of the workspace the page
// asks for, or says why it can't.
func (s *Server) workspaceFileHandler(w http.ResponseWriter, r *http.Request) {
	file, err := readWorkspaceFile(s.cfg.Workspace, r.URL.Query().Get("path"))
	var refused fileRefused
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, coloured(file))
	case errors.As(err, &refused):
		writeJSON(w, refusalStatus[refused.Kind], map[string]string{"error": refused.Message, "code": refused.Kind})
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// workspaceFilesHandler says which of the paths asked for (path=, repeated)
// are files of the workspace the page can show.
func (s *Server) workspaceFilesHandler(w http.ResponseWriter, r *http.Request) {
	files := viewableFiles(s.cfg.Workspace, r.URL.Query()["path"])
	if files == nil {
		files = []string{}
	}
	writeJSON(w, http.StatusOK, map[string][]string{"files": files})
}
