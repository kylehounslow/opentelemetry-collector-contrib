// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package genainormalizerprocessor

import (
	"encoding/json"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/genainormalizerprocessor/internal/otelsemconv"
)

// weaverImage pins the otel/weaver release run against the fixtures. Override
// with WEAVER_IMAGE to test an upgrade. Digest is otel/weaver v0.23.0.
const defaultWeaverImage = "otel/weaver@sha256:7984ecb55b859eb3034ae9d836c4eeda137e2bdd0873b7ba2bb6c3d24d6ff457"

// undefinedEnumVariant is Weaver's advice id for an enum attribute whose value
// the registry doesn't define. Weaver reports it at information level, so it
// doesn't change the exit code; the gate inspects the statistics directly.
const undefinedEnumVariant = "undefined_enum_variant"

// liveCheckReport is the subset of Weaver's live_check.json this test reads.
type liveCheckReport struct {
	Statistics struct {
		AdviceLevelCounts      map[string]int `json:"advice_level_counts"`
		AdviceTypeCounts       map[string]int `json:"advice_type_counts"`
		SeenRegistryAttributes map[string]int `json:"seen_registry_attributes"`
	} `json:"statistics"`
}

// TestWeaverLiveCheck validates the gen_ai.* attributes the processor emits
// against the semconv registry pinned by otelsemconv.SchemaURL, via Weaver's
// `registry live-check`. Integration build tag only; skips without Docker.
//
// Conformant fixtures must report no violations and no undefined enum
// variants. The negative-control fixture (openllmetry_workflow) carries
// invoke_workflow, undefined in the pinned registry's enum, so it must report
// exactly the undefined-enum-variant finding. That finding also confirms the
// pinned registry actually resolved.
func TestWeaverLiveCheck(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found on PATH; skipping Weaver live-check")
	}

	image := defaultWeaverImage
	if v := os.Getenv("WEAVER_IMAGE"); v != "" {
		image = v
	}
	// otelsemconv.SchemaURL is https://opentelemetry.io/schemas/<version>;
	// the registry ref must track the same semconv version.
	registry := "https://github.com/open-telemetry/semantic-conventions.git@v" +
		path.Base(otelsemconv.SchemaURL) + "[model]"

	cases := []struct {
		fixture string
		// wantEnumDrift marks the negative-control fixture, which must report
		// an undefined enum variant. The conformant fixtures must not.
		wantEnumDrift bool
	}{
		{fixture: "openinference"},
		{fixture: "openllmetry"},
		{fixture: "mixed"},
		{fixture: "openllmetry_workflow", wantEnumDrift: true},
	}

	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			report := runLiveCheck(t, image, registry, c.fixture)

			enumDrift := report.Statistics.AdviceTypeCounts[undefinedEnumVariant]
			violations := report.Statistics.AdviceLevelCounts["violation"]

			assert.Zero(t, violations,
				"fixture %s produced %d semconv violation(s)", c.fixture, violations)

			if c.wantEnumDrift {
				assert.Positive(t, enumDrift,
					"negative-control %s reported no enum drift (registry %s)", c.fixture, registry)
				return
			}

			assert.Zero(t, enumDrift,
				"fixture %s emits a gen_ai.* enum value undefined in the pinned registry", c.fixture)
			// The registry resolved iff Weaver recognized the attributes we
			// sent. Every conformant fixture carries gen_ai.request.model.
			assert.Positive(t, report.Statistics.SeenRegistryAttributes["gen_ai.request.model"],
				"pinned registry (%s) did not resolve: gen_ai.request.model unrecognized", registry)
		})
	}
}

// runLiveCheck runs `weaver registry live-check` on one fixture and returns the
// parsed report. Weaver writes live_check.json into the output directory.
func runLiveCheck(t *testing.T, image, registry, fixture string) liveCheckReport {
	t.Helper()

	fixtureDir, err := filepath.Abs(filepath.Join("testdata", "weaver"))
	require.NoError(t, err)
	outDir := t.TempDir()

	//nolint:gosec // image, registry, and fixture are test-controlled constants
	cmd := exec.CommandContext(t.Context(), "docker", "run", "--rm",
		"--mount", "type=bind,source="+fixtureDir+",target=/data,readonly",
		"--mount", "type=bind,source="+outDir+",target=/out",
		image, "registry", "live-check",
		"--registry", registry,
		"--input-source", "/data/"+fixture+".json",
		"--input-format", "json",
		"--format", "json",
		"--output", "/out",
	)
	out, err := cmd.CombinedOutput()
	// A non-zero exit means a violation-level finding, which the per-case
	// assertions report with more context. Surface the output regardless so a
	// registry-resolution or Docker failure is debuggable.
	t.Logf("weaver live-check %s output:\n%s", fixture, out)
	if err != nil {
		require.FileExistsf(t, filepath.Join(outDir, "live_check.json"), "weaver run failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "live_check.json"))
	require.NoError(t, err)

	var report liveCheckReport
	require.NoError(t, json.Unmarshal(data, &report))
	return report
}
