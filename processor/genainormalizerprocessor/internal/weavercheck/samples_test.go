// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package weavercheck

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func TestTracesToSamples_SingleSpanWithResource(t *testing.T) {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "my-svc")
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("chat my-model")
	span.SetKind(ptrace.SpanKindClient)
	span.Attributes().PutStr("gen_ai.operation.name", "chat")
	span.Attributes().PutInt("gen_ai.usage.input_tokens", 100)

	samples := TracesToSamples(td)

	got, err := json.Marshal(samples)
	require.NoError(t, err)

	want := `[
		{"resource": {"attributes": [{"name": "service.name", "value": "my-svc"}]}},
		{"span": {
			"name": "chat my-model",
			"kind": "client",
			"attributes": [
				{"name": "gen_ai.operation.name", "value": "chat"},
				{"name": "gen_ai.usage.input_tokens", "value": 100}
			]
		}}
	]`
	assert.JSONEq(t, want, string(got))
}

func TestTracesToSamples_EmptyResourceOmitsResourceSample(t *testing.T) {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("op")
	span.SetKind(ptrace.SpanKindInternal)

	samples := TracesToSamples(td)

	got, err := json.Marshal(samples)
	require.NoError(t, err)

	want := `[{"span": {"name": "op", "kind": "internal", "attributes": []}}]`
	assert.JSONEq(t, want, string(got))
}

func TestTracesToSamples_AttributeTypes(t *testing.T) {
	td := ptrace.NewTraces()
	span := td.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("op")
	span.SetKind(ptrace.SpanKindInternal)
	attrs := span.Attributes()
	attrs.PutStr("s", "v")
	attrs.PutInt("i", 7)
	attrs.PutDouble("f", 1.5)
	attrs.PutBool("b", true)
	sl := attrs.PutEmptySlice("arr")
	sl.AppendEmpty().SetStr("a")
	sl.AppendEmpty().SetStr("b")

	samples := TracesToSamples(td)
	got, err := json.Marshal(samples)
	require.NoError(t, err)

	want := `[{"span": {
		"name": "op",
		"kind": "internal",
		"attributes": [
			{"name": "s", "value": "v"},
			{"name": "i", "value": 7},
			{"name": "f", "value": 1.5},
			{"name": "b", "value": true},
			{"name": "arr", "value": ["a", "b"]}
		]
	}}]`
	assert.JSONEq(t, want, string(got))
}

func TestTracesToSamples_MultipleSpansShareResource(t *testing.T) {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "svc")
	ss := rs.ScopeSpans().AppendEmpty()
	s1 := ss.Spans().AppendEmpty()
	s1.SetName("a")
	s1.SetKind(ptrace.SpanKindInternal)
	s2 := ss.Spans().AppendEmpty()
	s2.SetName("b")
	s2.SetKind(ptrace.SpanKindServer)

	samples := TracesToSamples(td)
	got, err := json.Marshal(samples)
	require.NoError(t, err)

	want := `[
		{"resource": {"attributes": [{"name": "service.name", "value": "svc"}]}},
		{"span": {"name": "a", "kind": "internal", "attributes": []}},
		{"span": {"name": "b", "kind": "server", "attributes": []}}
	]`
	assert.JSONEq(t, want, string(got))
}
