package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

const gatewayBaseURL = "https://ai-gateway.vercel.sh/v1"

// Recommendation is the response contract every caller in your stack relies on.
type Recommendation struct {
	City            string `json:"city"`
	Summary         string `json:"summary"`
	Recommendations []struct {
		Name             string `json:"name"`
		Category         string `json:"category"`
		Description      string `json:"description"`
		BestTime         string `json:"bestTime"`
		WeatherDependent bool   `json:"weatherDependent"`
	} `json:"recommendations"`
}

// recommendationSchema is the JSON Schema version of Recommendation, used for
// structured output so the model's answer always parses into the struct above.
var recommendationSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"city":    map[string]any{"type": "string"},
		"summary": map[string]any{"type": "string", "description": "Two or three sentences on what the city is like right now"},
		"recommendations": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":             map[string]any{"type": "string"},
					"category":         map[string]any{"type": "string", "enum": []string{"food", "culture", "outdoors", "nightlife", "shopping", "family"}},
					"description":      map[string]any{"type": "string"},
					"bestTime":         map[string]any{"type": "string", "description": "Time of day or day of trip that suits this best"},
					"weatherDependent": map[string]any{"type": "boolean"},
				},
				"required":             []string{"name", "category", "description", "bestTime", "weatherDependent"},
				"additionalProperties": false,
			},
		},
	},
	"required":             []string{"city", "summary", "recommendations"},
	"additionalProperties": false,
}

const instructions = `You are a travel concierge who recommends things to do in a city.
Always call the get_forecast tool first so you can favor indoor activities on wet days and outdoor ones on dry days.
Prefer specific, named places over generic advice. Match the traveler's stated interests.
Return between 3 and 8 recommendations.`

// Agent wraps an AI Gateway client and the model it talks to. Credentials are
// supplied per request (see Recommend), because on Vercel the OIDC token is
// delivered on each incoming request and rotates over time.
type Agent struct {
	client openai.Client
	model  string
}

// NewAgent builds a client pointed at AI Gateway. Extra options are appended,
// which lets tests swap the base URL for a fake server.
func NewAgent(model string, opts ...option.RequestOption) *Agent {
	base := []option.RequestOption{option.WithBaseURL(gatewayBaseURL)}
	return &Agent{client: openai.NewClient(append(base, opts...)...), model: model}
}

// Recommend runs the tool-calling loop: ask the model, execute any tool it
// requests, feed the result back, and repeat until it returns the final JSON.
// credential is an AI Gateway API key or a Vercel OIDC token.
func (a *Agent) Recommend(ctx context.Context, credential, city string, days int, interests []string) (*Recommendation, error) {
	if credential == "" {
		return nil, errors.New("no AI Gateway credential")
	}
	auth := option.WithAPIKey(credential)

	prompt := fmt.Sprintf("A traveler is spending %d day(s) in %s.", days, city)
	if len(interests) > 0 {
		prompt += fmt.Sprintf(" They are interested in: %v.", interests)
	}

	params := openai.ChatCompletionNewParams{
		Model: shared.ChatModel(a.model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(instructions),
			openai.UserMessage(prompt),
		},
		Tools: []openai.ChatCompletionToolUnionParam{
			openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
				Name:        "get_forecast",
				Description: openai.String("Get the 7-day weather forecast for a city."),
				Parameters:  weatherToolSchema,
			}),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
				JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:   "recommendation",
					Schema: recommendationSchema,
					Strict: openai.Bool(true),
				},
			},
		},
	}

	const maxSteps = 5
	for step := 0; step < maxSteps; step++ {
		completion, err := a.client.Chat.Completions.New(ctx, params, auth)
		if err != nil {
			return nil, fmt.Errorf("ai gateway: %w", err)
		}
		msg := completion.Choices[0].Message

		// No tool calls means the model has produced its final answer.
		if len(msg.ToolCalls) == 0 {
			var rec Recommendation
			if err := json.Unmarshal([]byte(msg.Content), &rec); err != nil {
				return nil, fmt.Errorf("model returned invalid JSON: %w", err)
			}
			return &rec, nil
		}

		// Otherwise execute each requested tool and append the results.
		params.Messages = append(params.Messages, msg.ToParam())
		for _, call := range msg.ToolCalls {
			result := runTool(ctx, call.Function.Name, call.Function.Arguments)
			params.Messages = append(params.Messages, openai.ToolMessage(result, call.ID))
		}
	}
	return nil, fmt.Errorf("agent did not finish within %d steps", maxSteps)
}

// runTool dispatches a tool call by name and always returns a string the
// model can read, including on error, so the loop can recover.
func runTool(ctx context.Context, name, rawArgs string) string {
	switch name {
	case "get_forecast":
		var args struct {
			City string `json:"city"`
		}
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			return `{"error":"invalid arguments"}`
		}
		f, err := getForecast(ctx, args.City)
		if err != nil {
			return fmt.Sprintf(`{"error":%q}`, err.Error())
		}
		b, _ := json.Marshal(f)
		return string(b)
	default:
		return fmt.Sprintf(`{"error":"unknown tool %q"}`, name)
	}
}
