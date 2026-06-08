// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package genainormalizerprocessor

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	oisemconv "github.com/Arize-ai/openinference/go/openinference-semantic-conventions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	semconvai "github.com/traceloop/go-openllmetry/semconv-ai"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/processor/processortest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/genainormalizerprocessor/internal/metadata"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/genainormalizerprocessor/internal/openinference"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/genainormalizerprocessor/internal/openllmetry"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/genainormalizerprocessor/internal/weavercheck"
)

var updateWeaverFixtures = flag.Bool("update-weaver-fixtures", false,
	"regenerate Weaver live-check sample fixtures under testdata/weaver/")

// weaverFixtureCases are the inputs the Weaver live-check fixtures are
// generated from. The inputs are written here rather than reused from the
// benchmark helpers so the conformance check and the throughput benchmarks
// can evolve independently: a benchmark tuned for span volume must not
// silently change what live-check validates.
//
// Each conformant case feeds the full set of source attributes the built-in
// normalizer recognizes, with values chosen to satisfy the pinned semconv
// registry (valid enum members, spec-typed values). The workflow case is the
// exception: it is a negative control. openllmetry folds
// traceloop.span.kind=workflow onto gen_ai.operation.name=invoke_workflow,
// which is not a member of the gen_ai.operation.name enum in the pinned
// registry. It exists so the integration test can prove enum-drift detection
// fires and that the pinned registry is the one actually in effect.
func weaverFixtureCases() []struct {
	name    string
	sources []Source
	traces  ptrace.Traces
} {
	return []struct {
		name    string
		sources []Source
		traces  ptrace.Traces
	}{
		{"openinference", []Source{{Name: SourceOpenInference}}, openInferenceFixtureTrace()},
		{"openllmetry", []Source{{Name: SourceOpenLLMetry}}, openLLMetryFixtureTrace()},
		{"mixed", []Source{{Name: SourceOpenInference}, {Name: SourceOpenLLMetry}}, mixedFixtureTrace()},
		{"openllmetry_workflow", []Source{{Name: SourceOpenLLMetry}}, openLLMetryWorkflowFixtureTrace()},
	}
}

// openInferenceFixtureTrace feeds every OpenInference source key the
// normalizer maps (see internal/openinference/mappings.go), with conformant
// values. openinference.span.kind=LLM folds to gen_ai.operation.name=chat.
func openInferenceFixtureTrace() ptrace.Traces {
	td := newGenAISpan()
	attrs := spanAttrs(td)
	attrs.PutInt(oisemconv.LLMTokenCountPrompt, 100)
	attrs.PutInt(oisemconv.LLMTokenCountCompletion, 200)
	attrs.PutStr(oisemconv.LLMModelName, "claude-sonnet-4")
	attrs.PutStr(oisemconv.LLMProvider, "anthropic")
	attrs.PutStr(oisemconv.LLMInputMessages, `[{"role":"user"}]`)
	attrs.PutStr(oisemconv.LLMOutputMessages, `[{"role":"assistant"}]`)
	attrs.PutStr(oisemconv.ToolName, "search")
	attrs.PutStr(oisemconv.ToolDescription, "web search")
	attrs.PutStr(oisemconv.ToolCallFunctionArgumentsJSON, `{"q":"otel"}`)
	attrs.PutStr(oisemconv.ToolCallID, "call-1")
	attrs.PutStr(oisemconv.AgentName, "research-agent")
	attrs.PutStr(oisemconv.SessionID, "sess-123")
	attrs.PutStr(oisemconv.OpenInferenceSpanKind, oisemconv.SpanKindLLM)
	return td
}

// openLLMetryFixtureTrace feeds every OpenLLMetry source key the normalizer
// maps (see internal/openllmetry/mappings.go), with conformant values.
// llm.request.type=chat folds to gen_ai.operation.name=chat.
func openLLMetryFixtureTrace() ptrace.Traces {
	td := newGenAISpan()
	attrs := spanAttrs(td)
	attrs.PutInt(string(semconvai.LLMUsagePromptTokens), 100)
	attrs.PutInt(string(semconvai.LLMUsageCompletionTokens), 200)
	attrs.PutStr(string(semconvai.LLMRequestModel), "claude-sonnet-4")
	attrs.PutStr(string(semconvai.LLMResponseModel), "claude-sonnet-4")
	attrs.PutInt(string(semconvai.LLMRequestMaxTokens), 1024)
	attrs.PutDouble(openllmetry.KeyRequestTemperature, 0.7)
	attrs.PutDouble(openllmetry.KeyRequestTopP, 0.9)
	attrs.PutInt(string(semconvai.LLMTopK), 40)
	attrs.PutDouble(string(semconvai.LLMFrequencyPenalty), 0.1)
	attrs.PutDouble(string(semconvai.LLMPresencePenalty), 0.2)
	attrs.PutStr(string(semconvai.LLMChatStopSequence), "STOP")
	attrs.PutStr(string(semconvai.LLMRequestFunctions), `[{"name":"f"}]`)
	attrs.PutStr(openllmetry.KeyResponseFinishReason, "stop")
	attrs.PutStr(string(semconvai.TraceloopEntityName), "research-agent")
	attrs.PutStr(openllmetry.KeyEntityInput, `[{"role":"user"}]`)
	attrs.PutStr(openllmetry.KeyEntityOutput, `[{"role":"assistant"}]`)
	attrs.PutStr(string(semconvai.LLMRequestType), "chat")
	return td
}

// mixedFixtureTrace carries attributes for both built-in sources on one span,
// the realistic case where a span is annotated by more than one convention.
func mixedFixtureTrace() ptrace.Traces {
	td := newGenAISpan()
	attrs := spanAttrs(td)
	// openinference
	attrs.PutStr(oisemconv.LLMModelName, "claude-sonnet-4")
	attrs.PutStr(oisemconv.LLMProvider, "anthropic")
	attrs.PutStr(oisemconv.OpenInferenceSpanKind, oisemconv.SpanKindLLM)
	// openllmetry
	attrs.PutInt(string(semconvai.LLMUsagePromptTokens), 100)
	attrs.PutInt(string(semconvai.LLMUsageCompletionTokens), 200)
	attrs.PutStr(string(semconvai.LLMRequestType), "chat")
	return td
}

// openLLMetryWorkflowFixtureTrace is a negative control. The workflow span
// kind folds to gen_ai.operation.name=invoke_workflow, which the pinned
// registry's enum does not define. See weaverFixtureCases for why it exists.
func openLLMetryWorkflowFixtureTrace() ptrace.Traces {
	td := newGenAISpan()
	attrs := spanAttrs(td)
	attrs.PutStr(string(semconvai.LLMRequestModel), "claude-sonnet-4")
	attrs.PutStr(string(semconvai.TraceloopSpanKind), "workflow")
	return td
}

func newGenAISpan() ptrace.Traces {
	td := ptrace.NewTraces()
	span := td.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	// semconv pins GenAI inference spans to client kind, and Weaver rejects a
	// span sample with no kind.
	span.SetKind(ptrace.SpanKindClient)
	return td
}

func spanAttrs(td ptrace.Traces) pcommon.Map {
	return td.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).Attributes()
}

// TestWeaverFixtures regenerates (with -update-weaver-fixtures) or verifies
// the Weaver live-check sample fixtures consumed by TestWeaverLiveCheck. The
// fixtures are processor output, so a normalization change the committed
// fixtures don't reflect fails this test, forcing a regeneration and a fresh
// look at what live-check validates.
func TestWeaverFixtures(t *testing.T) {
	for _, c := range weaverFixtureCases() {
		t.Run(c.name, func(t *testing.T) {
			out := normalizeForFixture(t, c.sources, c.traces)
			keepGenAIAttributesOnly(out)
			samples := weavercheck.TracesToSamples(out)

			got, err := json.MarshalIndent(samples, "", "  ")
			require.NoError(t, err)
			got = append(got, '\n')

			path := filepath.Join("testdata", "weaver", c.name+".json")
			if *updateWeaverFixtures {
				require.NoError(t, os.WriteFile(path, got, 0o600))
				return
			}

			want, err := os.ReadFile(path)
			require.NoError(t, err, "fixture missing; run: go test -run TestWeaverFixtures -update-weaver-fixtures")
			assert.JSONEq(t, string(want), string(got))
		})
	}
}

// TestWeaverFixtureCoverage fails if a built-in source's LookupTable contains a
// key no fixture feeds. It is the guard that lets the fixtures stay hand-written:
// add a mapping and forget the fixture, and this fails listing the key.
//
// synonymKeys are source keys that map to a target another key already covers
// (e.g. embedding.model_name and llm.model_name both fold to
// gen_ai.request.model). Feeding them on one span would collide under
// overwrite=false, so they are validated by their synonym, not independently.
// Listed explicitly so a new synonym is a deliberate addition here, not a silent
// omission.
func TestWeaverFixtureCoverage(t *testing.T) {
	synonymKeys := map[string]bool{
		oisemconv.EmbeddingModelName:      true, // -> gen_ai.request.model (via llm.model_name)
		oisemconv.RerankerModelName:       true, // -> gen_ai.request.model
		openllmetry.KeyResponseStopReason: true, // -> gen_ai.response.finish_reasons (via finish_reason)
	}

	// fed[sourceName] = set of source keys any fixture for that source supplies.
	fed := map[SourceName]map[string]bool{}
	for _, c := range weaverFixtureCases() {
		for _, src := range c.sources {
			keys := fed[src.Name]
			if keys == nil {
				keys = map[string]bool{}
				fed[src.Name] = keys
			}
			collectSpanAttrKeys(c.traces, keys)
		}
	}

	// Every built-in source's table must be listed here, or its keys go
	// unchecked.
	tables := map[SourceName]map[string]string{
		SourceOpenInference: openinference.LookupTable,
		SourceOpenLLMetry:   openllmetry.LookupTable,
	}
	for sourceName, table := range tables {
		for key := range table {
			if synonymKeys[key] {
				continue
			}
			assert.Truef(t, fed[sourceName][key],
				"no fixture feeds %s key %q; add it to a fixture or to synonymKeys", sourceName, key)
		}
	}
}

func collectSpanAttrKeys(td ptrace.Traces, out map[string]bool) {
	rss := td.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		sss := rss.At(i).ScopeSpans()
		for j := 0; j < sss.Len(); j++ {
			spans := sss.At(j).Spans()
			for k := 0; k < spans.Len(); k++ {
				for key := range spans.At(k).Attributes().All() {
					out[key] = true
				}
			}
		}
	}
}

// keepGenAIAttributesOnly drops every span attribute that is not in the
// gen_ai.* namespace. live-check validates the processor's contract (the
// gen_ai.* attributes it emits conform to semconv), not the vendor and noise
// attributes that pass through unchanged and are absent from the registry.
func keepGenAIAttributesOnly(td ptrace.Traces) {
	rss := td.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		sss := rss.At(i).ScopeSpans()
		for j := 0; j < sss.Len(); j++ {
			spans := sss.At(j).Spans()
			for k := 0; k < spans.Len(); k++ {
				spans.At(k).Attributes().RemoveIf(func(key string, _ pcommon.Value) bool {
					return !strings.HasPrefix(key, "gen_ai.")
				})
			}
		}
	}
}

func normalizeForFixture(t *testing.T, sources []Source, traces ptrace.Traces) ptrace.Traces {
	t.Helper()
	factory := NewFactory()
	cfg := &Config{Sources: sources}
	sink := new(consumertest.TracesSink)

	p, err := factory.CreateTraces(t.Context(), processortest.NewNopSettings(metadata.Type), cfg, sink)
	require.NoError(t, err)
	require.NoError(t, p.Start(t.Context(), componenttest.NewNopHost()))

	clone := ptrace.NewTraces()
	traces.CopyTo(clone)
	require.NoError(t, p.ConsumeTraces(t.Context(), clone))
	require.NoError(t, p.Shutdown(t.Context()))

	all := sink.AllTraces()
	require.Len(t, all, 1)
	return all[0]
}
