package tools

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

type BashTool struct {
	cwd string
}

func NewBashTool(cwd string) *BashTool {
	return &BashTool{cwd: cwd}
}

func (t *BashTool) Name() string { return "bash" }

func (t *BashTool) Description() string {
	return "Execute a bash command in the current working directory. Returns stdout and stderr. Output is truncated to last 2000 lines or 50KB (whichever is hit first). If truncated, full output is saved to a temp file. Optionally provide a timeout in seconds."
}

type BashDetails struct {
	Truncation     *Truncation `json:"truncation,omitempty"`
	FullOutputPath string      `json:"fullOutputPath,omitempty"`
}

func (t *BashTool) Execute(ctx context.Context, in BashInput, onUpdate StreamUpdate) (Result, error) {
	if strings.TrimSpace(in.Command) == "" {
		return Result{}, fmt.Errorf("command is required")
	}
	if _, err := os.Stat(t.cwd); err != nil {
		return Result{}, fmt.Errorf("working directory does not exist: %s", t.cwd)
	}

	ctxExec := ctx
	var cancel context.CancelFunc
	if in.Timeout != nil && *in.Timeout > 0 {
		ctxExec, cancel = context.WithTimeout(ctx, time.Duration(*in.Timeout)*time.Second)
		defer cancel()
	}

	cmd := buildShellCommand(ctxExec, in.Command)
	cmd.Dir = t.cwd

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Result{}, err
	}

	if err := cmd.Start(); err != nil {
		return Result{}, err
	}

	var (
		mu           sync.Mutex
		rolling      [][]byte
		rollingBytes int
		totalBytes   int
		tmpPath      string
		tmpFile      *os.File
	)

	appendChunk := func(data []byte) error {
		mu.Lock()
		defer mu.Unlock()

		totalBytes += len(data)
		if totalBytes > DefaultMaxBytes && tmpFile == nil {
			f, p, err := createTempBashLog()
			if err != nil {
				return err
			}
			tmpFile = f
			tmpPath = p
			for _, c := range rolling {
				if _, err := tmpFile.Write(c); err != nil {
					return err
				}
			}
		}
		if tmpFile != nil {
			if _, err := tmpFile.Write(data); err != nil {
				return err
			}
		}

		copyChunk := make([]byte, len(data))
		copy(copyChunk, data)
		rolling = append(rolling, copyChunk)
		rollingBytes += len(copyChunk)

		maxRollingBytes := DefaultMaxBytes * 2
		for rollingBytes > maxRollingBytes && len(rolling) > 1 {
			rollingBytes -= len(rolling[0])
			rolling = rolling[1:]
		}

		if onUpdate != nil {
			joined := bytes.Join(rolling, nil)
			preview, trunc := truncateTail(string(joined), DefaultMaxLines, DefaultMaxBytes)
			d := BashDetails{}
			if trunc.Truncated {
				d.Truncation = &trunc
			}
			if tmpPath != "" {
				d.FullOutputPath = tmpPath
			}
			onUpdate(Result{Content: []ContentPart{{Type: ContentPartText, Text: preview}}, Details: d})
		}

		return nil
	}

	readPipe := func(r io.Reader, wg *sync.WaitGroup, errCh chan<- error) {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				if appendErr := appendChunk(buf[:n]); appendErr != nil {
					errCh <- appendErr
					return
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					errCh <- err
				}
				return
			}
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	wg.Add(2)
	go readPipe(stdout, &wg, errCh)
	go readPipe(stderr, &wg, errCh)

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	var execErr error
	select {
	case execErr = <-waitCh:
	case execErr = <-errCh:
		killCommand(cmd)
	case <-ctxExec.Done():
		killCommand(cmd)
		execErr = ctxExec.Err()
	}

	wg.Wait()
	close(errCh)

	if tmpFile != nil {
		_ = tmpFile.Close()
	}

	mu.Lock()
	joined := bytes.Join(rolling, nil)
	fullOutput := string(joined)
	tmpOutputPath := tmpPath
	mu.Unlock()

	preview, trunc := truncateTail(fullOutput, DefaultMaxLines, DefaultMaxBytes)
	details := BashDetails{}
	if trunc.Truncated {
		details.Truncation = &trunc
		if tmpOutputPath != "" {
			details.FullOutputPath = tmpOutputPath
		}
		startLine := trunc.TotalLines - trunc.OutputLines + 1
		endLine := trunc.TotalLines
		if trunc.LastLinePartial {
			lastLine := ""
			scanner := bufio.NewScanner(strings.NewReader(fullOutput))
			for scanner.Scan() {
				lastLine = scanner.Text()
			}
			preview += fmt.Sprintf("\n\n[Showing last %s of line %d (line is %s). Full output: %s]", formatSize(trunc.OutputBytes), endLine, formatSize(len([]byte(lastLine))), tmpOutputPath)
		} else if trunc.TruncatedBy == TruncationByLines {
			preview += fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Full output: %s]", startLine, endLine, trunc.TotalLines, tmpOutputPath)
		} else {
			preview += fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%s limit). Full output: %s]", startLine, endLine, trunc.TotalLines, formatSize(DefaultMaxBytes), tmpOutputPath)
		}
	}

	if preview == "" {
		preview = "(no output)"
	}

	if execErr != nil {
		if errors.Is(execErr, context.DeadlineExceeded) {
			preview += "\n\nCommand timed out"
		} else if errors.Is(execErr, context.Canceled) {
			preview += "\n\nCommand aborted"
		} else if ee := (&exec.ExitError{}); errors.As(execErr, &ee) {
			preview += fmt.Sprintf("\n\nCommand exited with code %d", ee.ExitCode())
		} else {
			preview += "\n\n" + execErr.Error()
		}
		return Result{Content: []ContentPart{{Type: ContentPartText, Text: preview}}, Details: details}, errors.New(preview)
	}

	return Result{Content: []ContentPart{{Type: ContentPartText, Text: preview}}, Details: details}, nil
}

func buildShellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", command)
	}
	bashPath := "/bin/bash"
	if _, err := os.Stat(bashPath); err != nil {
		if p, lookErr := exec.LookPath("bash"); lookErr == nil {
			bashPath = p
		} else {
			return exec.CommandContext(ctx, "sh", "-lc", command)
		}
	}
	cmd := exec.CommandContext(ctx, bashPath, "-lc", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

func killCommand(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		_ = cmd.Process.Kill()
		return
	}
	if cmd.Process.Pid > 0 {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

func createTempBashLog() (*os.File, string, error) {
	f, err := os.CreateTemp("", "synapta-bash-*.log")
	if err != nil {
		return nil, "", err
	}
	path, err := filepath.Abs(f.Name())
	if err != nil {
		return f, f.Name(), nil
	}
	return f, path, nil
}
