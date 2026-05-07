package service

import (
	"context"
	"fmt"
	"os"

	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/integration"
	"github.com/kaeawc/grit/internal/project"
)

// EntityKind identifies what kind of entity is being queried.
type EntityKind string

const (
	EntityKindModule            EntityKind = "module"
	EntityKindVariant           EntityKind = "variant"
	EntityKindAction            EntityKind = "action"
	EntityKindArtifact          EntityKind = "artifact"
	EntityKindMaterialization   EntityKind = "materialization"
	EntityKindClasspathSnapshot EntityKind = "classpathSnapshot"
)

// EntityRef identifies a single entity to look up.
type EntityRef struct {
	Kind EntityKind `json:"kind"`
	ID   string     `json:"id"`
}

// EntityResult is the unified result for a single-entity lookup. Exactly one of
// the kind-specific payload fields is populated, matching Ref.Kind.
type EntityResult struct {
	Repo              string                                   `json:"repo"`
	Ref               EntityRef                                `json:"ref"`
	ModelCacheKey     string                                   `json:"modelCacheKey,omitempty"`
	Module            *integration.ModuleByIDResult            `json:"module,omitempty"`
	Variant           *integration.VariantByIDResult           `json:"variant,omitempty"`
	Action            *integration.ActionByIDResult            `json:"action,omitempty"`
	Artifact          *integration.ArtifactByIDResult          `json:"artifact,omitempty"`
	Materialization   *integration.MaterializationByIDResult   `json:"materialization,omitempty"`
	ClasspathSnapshot *integration.ClasspathSnapshotByIDResult `json:"classpathSnapshot,omitempty"`
}

// EntityRelation identifies a relationship to surface for an entity.
type EntityRelation string

const (
	EntityRelationConsumers EntityRelation = "consumers"
)

// EntityRelationResult is the unified result for a single-entity relation
// lookup (e.g. consumers of a materialization or classpath snapshot).
type EntityRelationResult struct {
	Repo                       string                                            `json:"repo"`
	Ref                        EntityRef                                         `json:"ref"`
	Relation                   EntityRelation                                    `json:"relation"`
	ModelCacheKey              string                                            `json:"modelCacheKey,omitempty"`
	MaterializationConsumers   *integration.MaterializationConsumersResult       `json:"materializationConsumers,omitempty"`
	ClasspathSnapshotConsumers *integration.ClasspathSnapshotConsumersByIDResult `json:"classpathSnapshotConsumers,omitempty"`
}

// EntityByID returns the requested entity. Use this for new callers; the
// kind-specific XByID methods are kept as thin wrappers for backwards-compat.
func (s *Service) EntityByID(ctx context.Context, prj *project.Project, ref EntityRef) (EntityResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return EntityResult{}, err
	}
	out := EntityResult{
		Repo:          prj.RootDir,
		Ref:           ref,
		ModelCacheKey: view.CacheKey(),
	}
	switch ref.Kind {
	case EntityKindModule:
		r, ok := view.ModuleByID(graph.LogicalModuleID(ref.ID))
		if !ok {
			return EntityResult{}, os.ErrNotExist
		}
		out.Module = &r
	case EntityKindVariant:
		r, ok := view.VariantByID(graph.VariantID(ref.ID))
		if !ok {
			return EntityResult{}, os.ErrNotExist
		}
		out.Variant = &r
	case EntityKindAction:
		r, ok := view.ActionByID(graph.ActionID(ref.ID))
		if !ok {
			return EntityResult{}, os.ErrNotExist
		}
		out.Action = &r
	case EntityKindArtifact:
		r, ok := view.ArtifactByID(graph.ArtifactID(ref.ID))
		if !ok {
			return EntityResult{}, os.ErrNotExist
		}
		out.Artifact = &r
	case EntityKindMaterialization:
		r, ok := view.MaterializationByID(graph.MaterializationID(ref.ID))
		if !ok {
			return EntityResult{}, os.ErrNotExist
		}
		out.Materialization = &r
	case EntityKindClasspathSnapshot:
		r, ok := view.ClasspathSnapshotByID(ref.ID)
		if !ok {
			return EntityResult{}, os.ErrNotExist
		}
		out.ClasspathSnapshot = &r
	default:
		return EntityResult{}, fmt.Errorf("unknown entity kind %q", ref.Kind)
	}
	return out, nil
}

// EntityConsumers returns the consumers relation for an entity, where defined
// (currently Materialization and ClasspathSnapshot).
func (s *Service) EntityConsumers(ctx context.Context, prj *project.Project, ref EntityRef) (EntityRelationResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return EntityRelationResult{}, err
	}
	out := EntityRelationResult{
		Repo:          prj.RootDir,
		Ref:           ref,
		Relation:      EntityRelationConsumers,
		ModelCacheKey: view.CacheKey(),
	}
	switch ref.Kind {
	case EntityKindMaterialization:
		r, ok := view.MaterializationConsumers(graph.MaterializationID(ref.ID))
		if !ok {
			return EntityRelationResult{}, os.ErrNotExist
		}
		out.MaterializationConsumers = &r
	case EntityKindClasspathSnapshot:
		r, ok := view.ClasspathSnapshotConsumersByID(ref.ID)
		if !ok {
			return EntityRelationResult{}, os.ErrNotExist
		}
		out.ClasspathSnapshotConsumers = &r
	default:
		return EntityRelationResult{}, fmt.Errorf("entity kind %q has no consumers relation", ref.Kind)
	}
	return out, nil
}

// entityLookup runs the standard view-load + lookup + wrap dance shared by all
// per-entity By* methods. T is the integration-layer payload; R is the
// service-layer wrapped result.
func entityLookup[T any, R any](
	s *Service,
	ctx context.Context,
	prj *project.Project,
	id string,
	lookup func(*integration.ModelView, string) (T, bool),
	wrap func(repo, id, modelCacheKey string, payload T) R,
) (R, error) {
	var zero R
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return zero, err
	}
	payload, ok := lookup(view, id)
	if !ok {
		return zero, os.ErrNotExist
	}
	return wrap(prj.RootDir, id, view.CacheKey(), payload), nil
}
