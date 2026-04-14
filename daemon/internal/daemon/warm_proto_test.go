package daemon

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteUserMessage(t *testing.T) {
	var buf bytes.Buffer
	err := writeUserMessage(&buf, "hello world")
	require.NoError(t, err)

	line := strings.TrimSpace(buf.String())
	assert.Contains(t, line, `"type":"user"`)
	assert.Contains(t, line, `"role":"user"`)
	assert.Contains(t, line, `"content":"hello world"`)
}

func TestReadUntilInit(t *testing.T) {
	input := `{"type":"system","subtype":"api_retry","attempt":1}
{"type":"system","subtype":"init","session_id":"test-123"}
`
	scanner := bufio.NewScanner(strings.NewReader(input))
	err := readUntilInit(scanner)
	require.NoError(t, err)
}

func TestReadUntilInitEOF(t *testing.T) {
	input := `{"type":"system","subtype":"api_retry","attempt":1}
`
	scanner := bufio.NewScanner(strings.NewReader(input))
	err := readUntilInit(scanner)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no init message")
}

func TestReadUntilResultSuccess(t *testing.T) {
	input := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"thinking..."}]}}
{"type":"result","subtype":"success","result":"the answer is 42","is_error":false,"duration_ms":150}
`
	scanner := bufio.NewScanner(strings.NewReader(input))
	result, err := readUntilResult(scanner)
	require.NoError(t, err)
	assert.Equal(t, "the answer is 42", result.Result)
	assert.False(t, result.IsError)
	assert.Equal(t, 150, result.DurationMs)
}

func TestReadUntilResultError(t *testing.T) {
	input := `{"type":"result","subtype":"error_during_execution","result":"","is_error":true,"errors":["something broke"],"duration_ms":50}
`
	scanner := bufio.NewScanner(strings.NewReader(input))
	result, err := readUntilResult(scanner)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Equal(t, []string{"something broke"}, result.Errors)
}

func TestReadUntilResultEOF(t *testing.T) {
	input := `{"type":"assistant","message":{"role":"assistant","content":[]}}
`
	scanner := bufio.NewScanner(strings.NewReader(input))
	_, err := readUntilResult(scanner)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no result message")
}
