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

func TestReadUntilInitFailsAfterMalformedBurst(t *testing.T) {
	var sb strings.Builder
	// 101 malformed lines, then an init — exceeds the budget.
	for i := 0; i < 101; i++ {
		sb.WriteString("not json\n")
	}
	sb.WriteString(`{"type":"system","subtype":"init","session_id":"late"}` + "\n")

	scanner := bufio.NewScanner(strings.NewReader(sb.String()))
	err := readUntilInit(scanner)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "malformed")
}

func TestReadUntilInitToleratesSmallBurst(t *testing.T) {
	// A handful of malformed lines followed by init — under budget.
	input := "garbage\nmore garbage\n" +
		`{"type":"system","subtype":"init","session_id":"ok"}` + "\n"

	scanner := bufio.NewScanner(strings.NewReader(input))
	err := readUntilInit(scanner)
	require.NoError(t, err)
}

func TestReadUntilResultFailsAfterMalformedBurst(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 101; i++ {
		sb.WriteString("not json\n")
	}
	sb.WriteString(`{"type":"result","subtype":"success","result":"late","is_error":false,"duration_ms":1}` + "\n")

	scanner := bufio.NewScanner(strings.NewReader(sb.String()))
	_, err := readUntilResult(scanner)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "malformed")
}

func TestReadUntilResultResetsCounterOnValidLine(t *testing.T) {
	// Interleave malformed and valid-but-not-result lines; counter should
	// reset on each successful parse so a long stream survives.
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		sb.WriteString("garbage\n")
	}
	// A valid (non-result) line resets the counter.
	sb.WriteString(`{"type":"assistant","message":{"role":"assistant","content":[]}}` + "\n")
	for i := 0; i < 60; i++ {
		sb.WriteString("garbage\n")
	}
	sb.WriteString(`{"type":"result","subtype":"success","result":"ok","is_error":false,"duration_ms":1}` + "\n")

	scanner := bufio.NewScanner(strings.NewReader(sb.String()))
	result, err := readUntilResult(scanner)
	require.NoError(t, err)
	assert.Equal(t, "ok", result.Result)
}
