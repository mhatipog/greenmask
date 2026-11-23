package core

import "context"

// VirtualPlanningBuilder overlays validated logical relationships on a
// graph-only copy of physical introspection. The physical result remains
// untouched for schema drift, metadata and actual dump execution.
type VirtualPlanningBuilder interface {
	Build(
		ctx context.Context,
		session DatabaseSession,
		physical IntrospectionResult,
		keys []VirtualPrimaryKey,
		references []VirtualReference,
	) (IntrospectionResult, error)
}
