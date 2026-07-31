package llm

import (
	"context"
	"errors"
	"testing"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

type fakeChatCompletionClient struct {
	responses []fakeChatCompletionResult
	calls     int
}

type fakeChatCompletionResult struct {
	resp openai.ChatCompletionResponse
	err  error
}

func (f *fakeChatCompletionClient) CreateChatCompletion(_ context.Context, _ openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	idx := f.calls
	f.calls++
	if idx >= len(f.responses) {
		return openai.ChatCompletionResponse{}, errors.New("unexpected CreateChatCompletion call")
	}
	return f.responses[idx].resp, f.responses[idx].err
}

func TestParseSelectionFromToolCall(t *testing.T) {
	msg := openai.ChatCompletionMessage{
		ToolCalls: []openai.ToolCall{
			{
				ID:   "call_1",
				Type: openai.ToolTypeFunction,
				Function: openai.FunctionCall{
					Name:      selectionToolName,
					Arguments: `{"selection":"2"}`,
				},
			},
		},
	}
	got, err := parseSelection(msg)
	if err != nil {
		t.Fatalf("parseSelection returned error: %v", err)
	}
	if got != "2" {
		t.Fatalf("parseSelection returned %q", got)
	}
}

func TestParseSelectionFromDeprecatedFunctionCall(t *testing.T) {
	msg := openai.ChatCompletionMessage{
		FunctionCall: &openai.FunctionCall{
			Name:      selectionToolName,
			Arguments: `{"selection":"none_of_these"}`,
		},
	}
	got, err := parseSelection(msg)
	if err != nil {
		t.Fatalf("parseSelection returned error: %v", err)
	}
	if got != noneSelection {
		t.Fatalf("parseSelection returned %q", got)
	}
}

func TestParseSelectionMissingToolCall(t *testing.T) {
	msg := openai.ChatCompletionMessage{
		ToolCalls: []openai.ToolCall{
			{
				ID:   "call_1",
				Type: openai.ToolTypeFunction,
				Function: openai.FunctionCall{
					Name:      "other_function",
					Arguments: `{"selection":"2"}`,
				},
			},
		},
	}
	_, err := parseSelection(msg)
	if err == nil {
		t.Fatal("expected error when selection tool call is missing")
	}
}

func TestParseSelectionRejectsContentOnly(t *testing.T) {
	msg := openai.ChatCompletionMessage{
		Content: `{"selection":"none_of_these"}`,
	}
	_, err := parseSelection(msg)
	if err == nil {
		t.Fatal("expected error for content-only selection payload")
	}
}

func TestParseSelectionArgsInvalidJSON(t *testing.T) {
	_, err := parseSelectionArgs("{")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseSelectionArgsMissingField(t *testing.T) {
	_, err := parseSelectionArgs(`{"selection":""}`)
	if err == nil {
		t.Fatal("expected error for missing selection field")
	}
}

func TestParseSelectionArgsAcceptsNumericSelection(t *testing.T) {
	got, err := parseSelectionArgs(`{"selection":2}`)
	if err != nil {
		t.Fatalf("parseSelectionArgs returned error: %v", err)
	}
	if got != "2" {
		t.Fatalf("parseSelectionArgs returned %q", got)
	}
}

func TestParseSelectionArgsNormalizesNoneOfTheseVariants(t *testing.T) {
	got, err := parseSelectionArgs(`{"selection":"none of these"}`)
	if err != nil {
		t.Fatalf("parseSelectionArgs returned error: %v", err)
	}
	if got != noneSelection {
		t.Fatalf("parseSelectionArgs returned %q", got)
	}
}

func TestNormalizeSelectionDoesNotAssumeOptionCount(t *testing.T) {
	got := normalizeSelection("1", 0)
	if got != "1" {
		t.Fatalf("normalizeSelection returned %q", got)
	}
}

func TestNormalizeSelectionMapsNumberedNoneToNoneOfThese(t *testing.T) {
	got := normalizeSelection("13", 12)
	if got != noneSelection {
		t.Fatalf("normalizeSelection returned %q", got)
	}
}

func TestDescribeCreateChatCompletionErrorForRequestError(t *testing.T) {
	err := describeCreateChatCompletionError(&openai.RequestError{
		HTTPStatusCode: 503,
		Err:            errors.New("invalid character 'u' looking for beginning of value"),
	})
	got := err.Error()
	want := "chat completion request failed before tool parsing: endpoint returned HTTP 503 with a non-JSON error response: invalid character 'u' looking for beginning of value"
	if got != want {
		t.Fatalf("describeCreateChatCompletionError returned %q", got)
	}
}

func TestDescribeCreateChatCompletionErrorForAPIError(t *testing.T) {
	err := describeCreateChatCompletionError(&openai.APIError{
		HTTPStatusCode: 503,
		Message:        "That model is currently overloaded",
	})
	got := err.Error()
	want := "chat completion request failed: endpoint returned HTTP 503: That model is currently overloaded"
	if got != want {
		t.Fatalf("describeCreateChatCompletionError returned %q", got)
	}
}

func TestChooseOptionRetriesRetryableAPIError(t *testing.T) {
	client := &fakeChatCompletionClient{
		responses: []fakeChatCompletionResult{
			{
				err: &openai.APIError{
					HTTPStatusCode: 500,
					Message:        "Internal server error",
				},
			},
			{
				resp: openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{
						{
							Message: openai.ChatCompletionMessage{
								ToolCalls: []openai.ToolCall{
									{
										ID:   "call_1",
										Type: openai.ToolTypeFunction,
										Function: openai.FunctionCall{
											Name:      selectionToolName,
											Arguments: `{"selection":"1"}`,
										},
									},
								},
							},
						},
					},
					Usage: openai.Usage{
						PromptTokens:     11,
						CompletionTokens: 3,
						TotalTokens:      14,
					},
				},
			},
		},
	}

	sleepCalls := 0
	model := &OpenAIModel{
		client:         client,
		model:          "gpt-5.6-luna",
		maxAttempts:    3,
		retryBaseDelay: time.Millisecond,
		sleep: func(_ context.Context, _ time.Duration) error {
			sleepCalls++
			return nil
		},
	}

	result, err := model.ChooseOption(context.Background(), Prompt{
		Description: "cotton t-shirt",
		Options: []Option{
			{Name: "Shirts", FullName: "Apparel & Accessories > Clothing > Shirts", ID: "aa-1"},
		},
	})
	if err != nil {
		t.Fatalf("ChooseOption returned error: %v", err)
	}
	if client.calls != 2 {
		t.Fatalf("CreateChatCompletion calls = %d, want 2", client.calls)
	}
	if sleepCalls != 1 {
		t.Fatalf("sleep calls = %d, want 1", sleepCalls)
	}
	if result.Choice != "aa-1" {
		t.Fatalf("result choice = %q, want %q", result.Choice, "aa-1")
	}
}

func TestChooseOptionDoesNotRetryPermanentAPIError(t *testing.T) {
	client := &fakeChatCompletionClient{
		responses: []fakeChatCompletionResult{
			{
				err: &openai.APIError{
					HTTPStatusCode: 400,
					Message:        "Bad request",
				},
			},
		},
	}

	sleepCalls := 0
	model := &OpenAIModel{
		client:         client,
		model:          "gpt-5.6-luna",
		maxAttempts:    3,
		retryBaseDelay: time.Millisecond,
		sleep: func(_ context.Context, _ time.Duration) error {
			sleepCalls++
			return nil
		},
	}

	_, err := model.ChooseOption(context.Background(), Prompt{
		Description: "cotton t-shirt",
		Options: []Option{
			{Name: "Shirts", FullName: "Apparel & Accessories > Clothing > Shirts", ID: "aa-1"},
		},
	})
	if err == nil {
		t.Fatal("expected ChooseOption to return an error")
	}
	if client.calls != 1 {
		t.Fatalf("CreateChatCompletion calls = %d, want 1", client.calls)
	}
	if sleepCalls != 0 {
		t.Fatalf("sleep calls = %d, want 0", sleepCalls)
	}
	want := "chat completion request failed: endpoint returned HTTP 400: Bad request"
	if err.Error() != want {
		t.Fatalf("ChooseOption error = %q, want %q", err.Error(), want)
	}
}

func TestShouldRetryCreateChatCompletion(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "api 500",
			err: &openai.APIError{
				HTTPStatusCode: 500,
				Message:        "Internal server error",
			},
			want: true,
		},
		{
			name: "request 503",
			err: &openai.RequestError{
				HTTPStatusCode: 503,
				Err:            errors.New("upstream unavailable"),
			},
			want: true,
		},
		{
			name: "api 400",
			err: &openai.APIError{
				HTTPStatusCode: 400,
				Message:        "Bad request",
			},
			want: false,
		},
		{
			name: "plain error",
			err:  errors.New("boom"),
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldRetryCreateChatCompletion(tc.err); got != tc.want {
				t.Fatalf("shouldRetryCreateChatCompletion() = %v, want %v", got, tc.want)
			}
		})
	}
}
