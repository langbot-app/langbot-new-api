package relayconvert

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

// Real upstream shape (gemini-3.8-flash, 2026-11-09): one sentence split into
// several text parts, one carrying a thoughtSignature. The parts must be
// concatenated verbatim, not joined with "\n".
const splitTextGeminiResponse = `{"candidates":[{"content":{"role":"model","parts":[
 {"text":"- "},
 {"text":"7x24小时全天候即时响应客户。\n- 大"},
 {"thoughtSignature":"c2ln","text":"幅降低企业人工客服成本。\n- 同时高效处理海量咨询需求。"}
]},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":43,"candidatesTokenCount":35,"totalTokenCount":78}}`

const wantJoinedText = "- 7x24小时全天候即时响应客户。\n- 大幅降低企业人工客服成本。\n- 同时高效处理海量咨询需求。"

func decodeGemini(t *testing.T, raw string) *dto.GeminiChatResponse {
	t.Helper()
	var resp dto.GeminiChatResponse
	require.NoError(t, common.UnmarshalJsonStr(raw, &resp))
	return &resp
}

func TestResponseGeminiChat2OpenAIConcatenatesTextParts(t *testing.T) {
	out := ResponseGeminiChat2OpenAI("id", 1, decodeGemini(t, splitTextGeminiResponse))
	require.Len(t, out.Choices, 1)
	require.Equal(t, wantJoinedText, out.Choices[0].Message.StringContent())
}

func TestStreamResponseGeminiChat2OpenAIConcatenatesTextParts(t *testing.T) {
	out, _ := StreamResponseGeminiChat2OpenAI(decodeGemini(t, splitTextGeminiResponse))
	require.Len(t, out.Choices, 1)
	require.Equal(t, wantJoinedText, out.Choices[0].Delta.GetContentString())
}

func TestGeminiNewlineOnlyTextPartIsPreserved(t *testing.T) {
	raw := `{"candidates":[{"content":{"parts":[{"text":"第一行"},{"text":"\n"},{"text":"第二行"}]},"finishReason":"STOP","index":0}]}`
	out := ResponseGeminiChat2OpenAI("id", 1, decodeGemini(t, raw))
	require.Equal(t, "第一行\n第二行", out.Choices[0].Message.StringContent())
	stream, _ := StreamResponseGeminiChat2OpenAI(decodeGemini(t, raw))
	require.Equal(t, "第一行\n第二行", stream.Choices[0].Delta.GetContentString())
}

func TestGeminiThoughtPartsStaySeparateFromContent(t *testing.T) {
	raw := `{"candidates":[{"content":{"parts":[{"text":"think-a ","thought":true},{"text":"think-b","thought":true},{"text":"answer"}]},"finishReason":"STOP","index":0}]}`
	out := ResponseGeminiChat2OpenAI("id", 1, decodeGemini(t, raw))
	require.Equal(t, "answer", out.Choices[0].Message.StringContent())
	require.NotNil(t, out.Choices[0].Message.ReasoningContent)
	require.Equal(t, "think-a think-b", *out.Choices[0].Message.ReasoningContent)

	stream, _ := StreamResponseGeminiChat2OpenAI(decodeGemini(t, raw))
	require.Equal(t, "answer", stream.Choices[0].Delta.GetContentString())
	require.NotNil(t, stream.Choices[0].Delta.ReasoningContent)
	require.Equal(t, "think-a think-b", *stream.Choices[0].Delta.ReasoningContent)

	thoughtOnly := `{"candidates":[{"content":{"parts":[{"text":"only thinking","thought":true}]},"index":0}]}`
	stream, _ = StreamResponseGeminiChat2OpenAI(decodeGemini(t, thoughtOnly))
	require.Nil(t, stream.Choices[0].Delta.Content)
	require.Equal(t, "only thinking", *stream.Choices[0].Delta.ReasoningContent)
}

func TestGeminiMediaBlocksAreStillSeparated(t *testing.T) {
	raw := `{"candidates":[{"content":{"parts":[{"text":"see"},{"text":" below"},{"inlineData":{"mimeType":"image/png","data":"QUJD"}},{"text":"done"}]},"finishReason":"STOP","index":0}]}`
	out := ResponseGeminiChat2OpenAI("id", 1, decodeGemini(t, raw))
	require.Equal(t, "see below\n![image](data:image/png;base64,QUJD)\ndone", out.Choices[0].Message.StringContent())
}
