package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
)

const modelProxyMemoryBodyLimit = 1 << 20

type modelProxyBody struct {
	memory []byte
	path   string
	size   int64
}

func readLimitedProxyBody(r *http.Request) (*modelProxyBody, error) {
	defer r.Body.Close()
	reader := &io.LimitedReader{R: r.Body, N: modelProxyBodyLimit + 1}
	var memory bytes.Buffer
	if _, err := io.CopyN(&memory, reader, modelProxyMemoryBodyLimit+1); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if memory.Len() <= modelProxyMemoryBodyLimit && reader.N > 0 {
		return &modelProxyBody{memory: memory.Bytes(), size: int64(memory.Len())}, nil
	}

	temp, err := os.CreateTemp("", "cpa-helper-model-request-*")
	if err != nil {
		return nil, err
	}
	path := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(path)
	}
	if err := temp.Chmod(0o600); err != nil {
		cleanup()
		return nil, err
	}
	written, err := temp.Write(memory.Bytes())
	if err == nil {
		var copied int64
		copied, err = io.Copy(temp, reader)
		written += int(copied)
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	if int64(written) > modelProxyBodyLimit || reader.N <= 0 {
		_ = os.Remove(path)
		return nil, http.ErrBodyReadAfterClose
	}
	return &modelProxyBody{path: path, size: int64(written)}, nil
}

func (body *modelProxyBody) open() (io.ReadCloser, error) {
	if body.path != "" {
		return os.Open(body.path)
	}
	return io.NopCloser(bytes.NewReader(body.memory)), nil
}

func (body *modelProxyBody) close() {
	if body != nil && body.path != "" {
		_ = os.Remove(body.path)
	}
}

func modelFromProxyBody(body *modelProxyBody) string {
	if body == nil || body.size == 0 {
		return ""
	}
	reader, err := body.open()
	if err != nil {
		return ""
	}
	defer reader.Close()
	return scanTopLevelModel(reader)
}

func scanTopLevelModel(source io.Reader) string {
	const maxCapturedJSONString = 4096
	reader := bufio.NewReaderSize(source, 32<<10)
	depth := 0
	inString := false
	escaped := false
	expectKey := false
	awaitingValue := false
	captureKind := byte(0) // k for a top-level key, m for the model value.
	captureOverflow := false
	rawString := make([]byte, 0, 64)
	currentKey := ""

	for {
		char, err := reader.ReadByte()
		if err != nil {
			return ""
		}
		if inString {
			if captureKind != 0 && !captureOverflow {
				if len(rawString) >= maxCapturedJSONString {
					captureOverflow = true
				} else {
					rawString = append(rawString, char)
				}
			}
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
				continue
			}
			if char != '"' {
				continue
			}
			inString = false
			switch captureKind {
			case 'k':
				if !captureOverflow {
					_ = json.Unmarshal(rawString, &currentKey)
				}
			case 'm':
				if captureOverflow {
					return "\x00model_too_long"
				}
				var model string
				if json.Unmarshal(rawString, &model) != nil {
					return "\x00invalid_model"
				}
				return strings.TrimSpace(model)
			}
			captureKind = 0
			rawString = rawString[:0]
			captureOverflow = false
			continue
		}

		if depth == 0 {
			if char == '{' {
				depth = 1
				expectKey = true
			}
			continue
		}
		if char == '"' {
			inString = true
			escaped = false
			rawString = append(rawString[:0], '"')
			switch {
			case depth == 1 && expectKey:
				captureKind = 'k'
				expectKey = false
			case depth == 1 && awaitingValue && currentKey == "model":
				captureKind = 'm'
				awaitingValue = false
			default:
				captureKind = 0
				rawString = rawString[:0]
			}
			continue
		}
		switch char {
		case '{', '[':
			if depth == 1 && awaitingValue {
				if currentKey == "model" {
					return "\x00invalid_model"
				}
				awaitingValue = false
			}
			depth++
		case '}', ']':
			depth--
			if depth <= 0 {
				return ""
			}
		case ':':
			if depth == 1 && currentKey != "" {
				awaitingValue = true
			}
		case ',':
			if depth == 1 {
				currentKey = ""
				awaitingValue = false
				expectKey = true
			}
		default:
			if depth == 1 && awaitingValue && char != ' ' && char != '\t' && char != '\r' && char != '\n' {
				if currentKey == "model" {
					return "\x00invalid_model"
				}
				awaitingValue = false
			}
		}
	}
}
