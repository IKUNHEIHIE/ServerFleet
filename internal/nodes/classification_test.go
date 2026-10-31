package nodes_test

import (
	"errors"
	"slices"
	"testing"

	"serverfleet.local/serverfleet/internal/nodes"
)

func TestClassifySyntheticNodes(t *testing.T) {
	fixture := loadFixture(t)
	for _, tc := range []struct {
		dimension nodes.Dimension
		want      []nodes.Classification
	}{
		{
			dimension: nodes.DimensionRegion,
			want: []nodes.Classification{
				{Dimension: nodes.DimensionRegion, Value: "cn-east", Count: 2},
				{Dimension: nodes.DimensionRegion, Value: "cn-north", Count: 2},
			},
		},
		{
			dimension: nodes.DimensionProvider,
			want: []nodes.Classification{
				{Dimension: nodes.DimensionProvider, Value: "cloud-a", Count: 3},
				{Dimension: nodes.DimensionProvider, Value: "cloud-b", Count: 2},
			},
		},
		{
			dimension: nodes.DimensionTag,
			want: []nodes.Classification{
				{Dimension: nodes.DimensionTag, Value: "api", Count: 1},
				{Dimension: nodes.DimensionTag, Value: "cache", Count: 1},
				{Dimension: nodes.DimensionTag, Value: "db", Count: 1},
				{Dimension: nodes.DimensionTag, Value: "prod", Count: 3},
				{Dimension: nodes.DimensionTag, Value: "test", Count: 1},
				{Dimension: nodes.DimensionTag, Value: "web", Count: 1},
			},
		},
	} {
		t.Run(string(tc.dimension), func(t *testing.T) {
			before := mustJSON(t, fixture)
			got, err := nodes.Classify(fixture, tc.dimension)
			if err != nil {
				t.Fatalf("Classify returned error: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("Classify = %+v, want %+v", got, tc.want)
			}
			if after := mustJSON(t, fixture); string(before) != string(after) {
				t.Fatal("Classify mutated its input")
			}
		})
	}
}

func TestClassifyCountsRepeatedTagOnce(t *testing.T) {
	input := []nodes.Node{
		{ID: "a", Tags: []string{" web ", "web", ""}},
		{ID: "b", Tags: []string{"web", "db"}},
	}
	got, err := nodes.Classify(input, nodes.DimensionTag)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}
	want := []nodes.Classification{
		{Dimension: nodes.DimensionTag, Value: "db", Count: 1},
		{Dimension: nodes.DimensionTag, Value: "web", Count: 2},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Classify = %+v, want %+v", got, want)
	}
}

func TestClassifySkipsEmptyValues(t *testing.T) {
	input := []nodes.Node{
		{ID: "a", Region: "  ", Provider: ""},
		{ID: "b", Region: "cn-north", Provider: "cloud-a"},
	}
	for _, dimension := range []nodes.Dimension{nodes.DimensionRegion, nodes.DimensionProvider} {
		got, err := nodes.Classify(input, dimension)
		if err != nil {
			t.Fatalf("Classify returned error: %v", err)
		}
		if len(got) != 1 || got[0].Count != 1 {
			t.Fatalf("%s buckets = %+v, want one bucket with count 1", dimension, got)
		}
	}
}

func TestClassifyRejectsUnknownDimension(t *testing.T) {
	got, err := nodes.Classify(loadFixture(t), nodes.Dimension("datacenter"))
	if !errors.Is(err, nodes.ErrInvalidDimension) {
		t.Fatalf("error = %v, want ErrInvalidDimension", err)
	}
	if got != nil {
		t.Fatalf("result = %+v, want nil", got)
	}
}

func TestClassifyEmptyInput(t *testing.T) {
	got, err := nodes.Classify(nil, nodes.DimensionTag)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d buckets, want none", len(got))
	}
}
