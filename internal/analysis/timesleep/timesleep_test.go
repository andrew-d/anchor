package timesleep_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/andrew-d/anchor/internal/analysis/timesleep"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), timesleep.Analyzer, "a")
}
