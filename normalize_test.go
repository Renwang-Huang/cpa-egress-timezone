package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

const targetTimezone = "America/Los_Angeles"

func normalizeBody(raw []byte) ([]byte, error) {
	return normalizeBodyInZone(raw, time.Now(), targetTimezone)
}

func normalizeBodyAt(raw []byte, now time.Time) ([]byte, error) {
	return normalizeBodyInZone(raw, now, targetTimezone)
}

func TestClientContextAcrossResponsesChatAndUpdates(t *testing.T) {
	context := "<environment_context>\n<current_date>2026-10-19</current_date>\n<timezone>Asia/Hong_Kong</timezone>\n</environment_context>"
	cases := map[string]map[string]any{
		"responses":            {"input": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": context}}}}},
		"chat":                 {"messages": []any{map[string]any{"role": "system", "content": context}}},
		"input string":         {"input": context},
		"instructions":         {"instructions": context},
		"configuration update": {"input": []any{map[string]any{"type": "configuration_update", "configuration": map[string]any{"instructions": context}}}},
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(input)
			out, err := normalizeBodyAt(raw, time.Date(2026, 10, 21, 6, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(out, []byte("Asia/Hong_Kong")) || !bytes.Contains(out, []byte(targetTimezone)) {
				t.Fatalf("client timezone was not replaced: %s", out)
			}
			if !bytes.Contains(out, []byte("2026-10-19")) {
				t.Fatal("historical date was changed")
			}
			second, err := normalizeBodyAt(out, time.Date(2026, 10, 21, 6, 0, 0, 0, time.UTC))
			if err != nil || !bytes.Equal(out, second) {
				t.Fatal("normalization is not idempotent")
			}
		})
	}
}

func TestCurrentDateUsesLosAngelesCalendar(t *testing.T) {
	for _, tc := range []struct {
		name, instant, sourceZone, sourceDate, wantDate string
	}{
		{"PDT before midnight", "2026-10-21T06:30:00Z", "Asia/Hong_Kong", "2026-10-21", "2026-10-20"},
		{"PDT after midnight", "2026-10-21T07:00:00Z", "Asia/Hong_Kong", "2026-10-21", "2026-10-21"},
		{"PST year boundary", "2027-01-01T07:30:00Z", "Asia/Shanghai", "2027-01-01", "2026-12-31"},
		{"PST after midnight", "2027-01-01T08:00:00Z", "Asia/Shanghai", "2027-01-01", "2027-01-01"},
		{"DST begins", "2026-03-08T10:30:00Z", "Asia/Hong_Kong", "2026-03-08", "2026-03-08"},
		{"DST ends", "2026-11-01T09:30:00Z", "Asia/Hong_Kong", "2026-11-01", "2026-11-01"},
		{"historical date", "2026-10-21T06:30:00Z", "Asia/Hong_Kong", "2026-10-19", "2026-10-19"},
		{"already Los Angeles", "2026-10-21T06:30:00Z", targetTimezone, "2026-10-20", "2026-10-20"},
		{"unknown timezone", "2026-10-21T06:30:00Z", "Unknown/Zone", "2026-10-21", "2026-10-21"},
		{"empty timezone", "2026-10-21T06:30:00Z", "", "2026-10-21", "2026-10-21"},
		{"host local timezone", "2026-10-21T06:30:00Z", "Local", "2026-10-21", "2026-10-21"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, _ := time.Parse(time.RFC3339, tc.instant)
			context := "<environment_context><current_date>" + tc.sourceDate + "</current_date><timezone>" + tc.sourceZone + "</timezone></environment_context>"
			raw, _ := json.Marshal(map[string]any{"input": context})
			out, err := normalizeBodyAt(raw, now)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]string
			_ = json.Unmarshal(out, &got)
			want := "<environment_context><current_date>" + tc.wantDate + "</current_date><timezone>" + targetTimezone + "</timezone></environment_context>"
			if got["input"] != want {
				t.Fatalf("got %s, want %s", got["input"], want)
			}
			second, err := normalizeBodyAt(out, now)
			if err != nil || !bytes.Equal(out, second) {
				t.Fatal("current-date normalization is not idempotent")
			}
		})
	}
}

func TestOnlyLatestEnvironmentDateChanges(t *testing.T) {
	now := time.Date(2026, 10, 21, 6, 30, 0, 0, time.UTC)
	context := "<environment_context><current_date>2026-10-21</current_date><timezone>Asia/Hong_Kong</timezone></environment_context>"
	for name, payload := range map[string]map[string]any{
		"latest message": {"instructions": context, "input": []any{
			map[string]any{"role": "user", "content": context},
			map[string]any{"role": "user", "content": context}}},
		"latest content part": {"input": []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "input_text", "text": context},
			map[string]any{"type": "input_text", "text": context}}}}},
		"latest block": {"input": context + "\n" + context},
		"configuration update": {"input": []any{
			map[string]any{"role": "user", "content": context},
			map[string]any{"type": "configuration_update", "configuration": map[string]any{"instructions": context}}}},
	} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(payload)
			out, err := normalizeBodyAt(raw, now)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Count(out, []byte("2026-10-20")) != 1 || bytes.Count(out, []byte("2026-10-21")) != bytes.Count(raw, []byte("2026-10-21"))-1 {
				t.Fatalf("older environment dates changed: %s", out)
			}
			if bytes.Contains(out, []byte("Asia/Hong_Kong")) {
				t.Fatal("timezone declaration was missed")
			}
		})
	}
	// A stale latest environment must not cause an older same-day block to be
	// mistaken for the current environment. Ordinary dates also stay untouched.
	raw, _ := json.Marshal(map[string]any{"input": []any{
		map[string]any{"role": "user", "content": context},
		map[string]any{"role": "user", "content": strings.ReplaceAll(context, "2026-10-21", "2026-10-19") + " meeting on 2026-10-21"}}})
	out, err := normalizeBodyAt(raw, now)
	if err != nil || bytes.Contains(out, []byte("2026-10-20")) || !bytes.Contains(out, []byte("meeting on 2026-10-21")) {
		t.Fatalf("historical or ordinary date changed: %s (%v)", out, err)
	}
}

func TestExplicitLocationTimezoneAndOpaquePayload(t *testing.T) {
	raw := []byte(`{
	 "timezone":"Asia/Shanghai", "large_number":9007199254740993,
	 "client_metadata":{"timeZone":"Europe/London"},
	 "metadata":{"client_timezone":"UTC"},
	 "tools":[
	   {"type":"web_search","user_location":{"timezone":"Asia/Hong_Kong","city":"Los Angeles"}},
	   {"type":"function","name":"example","parameters":{"type":"object","properties":{"timezone":{"type":"string","default":"Asia/Shanghai"}}}}
	 ],
	 "input":[
	   {"type":"reasoning","encrypted_content":"opaque:Asia/Hong_Kong"},
	   {"type":"function_call","arguments":"{\"timezone\":\"Asia/Shanghai\"}"},
	   {"type":"message","role":"assistant","content":[{"type":"text","text":"<environment_context><timezone>Asia/Shanghai</timezone></environment_context>"}]},
	   {"type":"message","role":"user","content":[{"type":"input_text","text":"Please explain Asia/Hong_Kong and <timezone>Asia/Shanghai</timezone>."}]}
	 ]
	}`)
	out, err := normalizeBody(raw)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Timezone       string
		ClientMetadata map[string]string `json:"client_metadata"`
		Metadata       map[string]string
		Tools          []struct {
			Type         string
			UserLocation map[string]string `json:"user_location"`
			Parameters   json.RawMessage
		}
		Input []json.RawMessage
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Timezone != targetTimezone || got.ClientMetadata["timeZone"] != targetTimezone || got.Metadata["client_timezone"] != targetTimezone || got.Tools[0].UserLocation["timezone"] != targetTimezone {
		t.Fatalf("explicit timezone declarations not normalized: %s", out)
	}
	if !bytes.Contains(out, []byte("9007199254740993")) {
		t.Fatal("number precision lost")
	}
	if !bytes.Contains(got.Tools[1].Parameters, []byte("Asia/Shanghai")) {
		t.Fatal("tool schema changed")
	}
	var before struct{ Input []json.RawMessage }
	_ = json.Unmarshal(raw, &before)
	for i := range before.Input {
		var original, rewritten any
		_ = json.Unmarshal(before.Input[i], &original)
		_ = json.Unmarshal(got.Input[i], &rewritten)
		if !reflect.DeepEqual(original, rewritten) {
			t.Fatalf("opaque/history input %d changed", i)
		}
	}
}

func TestUnchangedRequestReturnsOriginalBytes(t *testing.T) {
	for _, raw := range []string{`{ "model": "test", "input": "Hello" }`, `{"timezone":null}`, `{"input":["message-id"]}`} {
		got, err := normalizeBody([]byte(raw))
		if err != nil || string(got) != raw {
			t.Fatalf("unexpected change: %s, %v", got, err)
		}
	}
}

func TestPluginFormatAndHeaderPolicy(t *testing.T) {
	previous := currentState()
	defer func() { stateMu.Lock(); activeState = previous; stateMu.Unlock() }()
	lifecycle, _ := json.Marshal(map[string]any{"config_yaml": []byte("mode: fixed\n")})
	if _, err := handleMethod("plugin.register", lifecycle); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(transformRequest{ToFormat: "codex", Body: []byte(`{"client_metadata":{"timezone":"Asia/Shanghai"}}`), Headers: map[string][]string{"X-Timezone": {"Asia/Shanghai"}, "Authorization": {"Bearer opaque"}}})
	res, err := handleMethod("request.intercept_after", raw)
	if err != nil {
		t.Fatal(err)
	}
	result := res.(map[string]any)
	if string(result["Body"].([]byte)) != `{"client_metadata":{"timezone":"America/Los_Angeles"}}` {
		t.Fatalf("body not normalized: %s", result["Body"])
	}
	headers := result["Headers"].(map[string][]string)
	if len(headers) != 1 || headers["X-Timezone"][0] != targetTimezone {
		t.Fatalf("wrong header overrides: %#v", headers)
	}
	for _, format := range []string{"codex", "openai", "openai-response"} {
		request, _ := json.Marshal(transformRequest{ToFormat: format, Body: []byte(`{"timezone":"Asia/Shanghai"}`)})
		x, err := handleMethod("request.normalize", request)
		if err != nil || !strings.Contains(string(x.(map[string]any)["Body"].([]byte)), targetTimezone) {
			t.Fatalf("format %s not handled", format)
		}
	}
	other := []byte(`{"timezone":"Asia/Shanghai"}`)
	request, _ := json.Marshal(transformRequest{ToFormat: "claude", Body: other})
	x, err := handleMethod("request.normalize", request)
	if err != nil || !bytes.Equal(x.(map[string]any)["Body"].([]byte), other) {
		t.Fatal("unrelated provider was changed")
	}
}
