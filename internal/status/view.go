package status

import "sync/atomic"

// GroupView is a bound view of one metric group. The binding is valid only
// after Freeze: before it, the index is rebuilt on every registration.
type GroupView struct {
	name    string
	members []metricRef
	visible *atomic.Int64
	gate    GroupGate
}

// Views binds every registered group in index order.
func (r *Registry) Views() []GroupView {
	groups := r.groups()
	views := make([]GroupView, len(groups))
	for i := range groups {
		views[i] = GroupView{
			name: groups[i].name, members: groups[i].members,
			visible: groups[i].visible, gate: groups[i].gate,
		}
	}
	return views
}

// VisibleViews omits empty roster slots while preserving sorted group order.
func (r *Registry) VisibleViews() []GroupView {
	all := r.Views()
	views := make([]GroupView, 0, len(all))
	for i := range all {
		if all[i].Visible() {
			views = append(views, all[i])
		}
	}
	return views
}

// GroupView binds one group by name for repeated lock-free reads
func (r *Registry) GroupView(name string) (GroupView, bool) {
	for _, g := range r.groups() {
		if g.name == name {
			return GroupView{name: g.name, members: g.members, visible: g.visible, gate: g.gate}, true
		}
	}
	return GroupView{}, false
}

// Name returns the group's key prefix
func (v GroupView) Name() string { return v.name }

// Len returns the metric count in the group
func (v GroupView) Len() int { return len(v.members) }

// Visible reports whether a gated group currently belongs in a view.
func (v GroupView) Visible() bool { return gateVisible(v.gate, v.visible, v.members) }

// MetricName returns the short name of member i
func (v GroupView) MetricName(i int) string { return v.members[i].name }

// Value returns the formatted current reading of member i
func (v GroupView) Value(i int) string { return v.members[i].display() }
