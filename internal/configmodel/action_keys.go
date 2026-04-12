package configmodel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/lint"
)

type actionCacheKeyFunc func(*Model, graph.Action) string

var actionCacheKeyRegistry = map[graph.ActionKind]actionCacheKeyFunc{
	graph.ActionKindLint: lintActionCacheKey,
}

func actionCacheKey(action graph.Action) string {
	return actionCacheKeyForModel(nil, action)
}

func actionCacheKeyForModel(m *Model, action graph.Action) string {
	if fn, ok := actionCacheKeyRegistry[action.Kind]; ok {
		if key := strings.TrimSpace(fn(m, action)); key != "" {
			return key
		}
	}
	return defaultActionCacheKey(action)
}

func lintActionCacheKey(m *Model, action graph.Action) string {
	if m == nil {
		return ""
	}
	modulePath := strings.TrimSpace(action.Attributes["modulePath"])
	variantName := strings.TrimSpace(action.Attributes["variantName"])
	if modulePath == "" || variantName == "" {
		return ""
	}
	resolved, ok := m.ResolvedVariant(modulePath, variantName)
	if !ok {
		return ""
	}
	return lint.ActionFromVariant(resolved).CacheKey().String()
}

func defaultActionCacheKey(action graph.Action) string {
	sum := sha256.New()
	parts := []string{
		action.ID.String(),
		action.ModuleID.String(),
		action.VariantID.String(),
		action.Name,
		string(action.Kind),
		action.Attributes["operation"],
		action.Attributes["variantName"],
		strings.Join(artifactIDs(action.Inputs), ","),
		strings.Join(artifactIDs(action.Outputs), ","),
		action.Note,
	}
	for _, part := range parts {
		fmt.Fprint(sum, part)
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}
