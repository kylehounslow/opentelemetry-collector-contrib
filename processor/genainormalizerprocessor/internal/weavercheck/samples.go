// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package weavercheck converts processor output traces into the JSON
// "samples" format consumed by Weaver's `registry live-check`, so emitted
// gen_ai.* attributes can be validated against a pinned semantic-conventions
// registry.
package weavercheck // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/genainormalizerprocessor/internal/weavercheck"

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type attributeSample struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

type spanSample struct {
	Name       string            `json:"name"`
	Kind       string            `json:"kind"`
	Attributes []attributeSample `json:"attributes"`
}

type resourceSample struct {
	Attributes []attributeSample `json:"attributes"`
}

// TracesToSamples flattens td into the Weaver live-check samples array. Each
// resource with attributes emits a {"resource": ...} element before the
// {"span": ...} elements of its spans. live-check treats the array as a flat
// stream, so resource attributes apply to the samples that follow.
func TracesToSamples(td ptrace.Traces) []any {
	var samples []any
	rss := td.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		rs := rss.At(i)
		if attrs := attributesOf(rs.Resource().Attributes()); len(attrs) > 0 {
			samples = append(samples, map[string]resourceSample{
				"resource": {Attributes: attrs},
			})
		}
		sss := rs.ScopeSpans()
		for j := 0; j < sss.Len(); j++ {
			spans := sss.At(j).Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				samples = append(samples, map[string]spanSample{
					"span": {
						Name:       span.Name(),
						Kind:       spanKind(span.Kind()),
						Attributes: attributesOf(span.Attributes()),
					},
				})
			}
		}
	}
	return samples
}

func attributesOf(m pcommon.Map) []attributeSample {
	attrs := make([]attributeSample, 0, m.Len())
	for k, v := range m.All() {
		attrs = append(attrs, attributeSample{Name: k, Value: v.AsRaw()})
	}
	return attrs
}

func spanKind(k ptrace.SpanKind) string {
	switch k {
	case ptrace.SpanKindInternal:
		return "internal"
	case ptrace.SpanKindServer:
		return "server"
	case ptrace.SpanKindClient:
		return "client"
	case ptrace.SpanKindProducer:
		return "producer"
	case ptrace.SpanKindConsumer:
		return "consumer"
	default:
		return "unspecified"
	}
}
