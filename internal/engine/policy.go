package engine

import "strings"

// PolicySource tracks where a resolved policy value originated.
type PolicySource string

const (
	SourceDefault       PolicySource = "default"
	SourceTemplate      PolicySource = "template"
	SourceSubscription  PolicySource = "subscription"
	SourceResourceGroup PolicySource = "resource_group"
	SourceResource      PolicySource = "resource"
)

// PolicyOrigin records the source level and the entity name.
type PolicyOrigin struct {
	Source PolicySource `json:"source"`
	Name   string       `json:"name,omitempty"`
}

func (o PolicyOrigin) String() string {
	if o.Name == "" {
		return string(o.Source)
	}
	return string(o.Source) + " \"" + o.Name + "\""
}

// ResolvedPolicy extends ResourcePolicy with full source tracking,
// inheritance metadata, and drift detection.
type ResolvedPolicy struct {
	ResourcePolicy
	ModeOrigin        PolicyOrigin  `json:"mode_origin"`
	CriticalityOrigin PolicyOrigin  `json:"criticality_origin"`
	ExternalOrigin    PolicyOrigin  `json:"external_origin"`
	Override          bool          `json:"override"`
	Drifts            []PolicyDrift `json:"drifts,omitempty"`
}

// PolicyDrift records a divergence between a resource's policy
// and its parent (RG or Subscription).
type PolicyDrift struct {
	Field         string       `json:"field"`
	ResourceValue string       `json:"resource_value"`
	ParentValue   string       `json:"parent_value"`
	ParentSource  PolicySource `json:"parent_source"`
	ParentName    string       `json:"parent_name"`
}

// PolicyTemplate is a named preset that can be referenced via
// the inframind-template tag. Scales governance across thousands
// of resources without tagging each one individually.
type PolicyTemplate struct {
	Mode         PolicyMode  `json:"mode"`
	Criticality  Criticality `json:"criticality"`
	ExternalDeps bool        `json:"external_deps"`
}

// BuiltinTemplates ships with InfraMind out of the box.
var BuiltinTemplates = map[string]PolicyTemplate{
	"production":  {Mode: ModeProtect, Criticality: CriticalityHigh},
	"staging":     {Mode: ModeObserve, Criticality: CriticalityMedium},
	"development": {Mode: ModeDefault, Criticality: CriticalityLow},
	"legacy":      {Mode: ModeProtect, Criticality: CriticalityHigh, ExternalDeps: true},
}

// TagHierarchy holds tags at each level of the cloud resource hierarchy.
// The PolicyResolver walks this from resource → RG → subscription to
// resolve inherited values.
type TagHierarchy struct {
	SubscriptionTags  map[string]string
	SubscriptionName  string
	ResourceGroupTags map[string]string
	ResourceGroupName string
	ResourceTags      map[string]string
}

// PolicyResolver resolves policies using inheritance, templates,
// override control, and drift detection.
type PolicyResolver struct {
	templates map[string]PolicyTemplate
}

func NewPolicyResolver() *PolicyResolver {
	return &PolicyResolver{templates: BuiltinTemplates}
}

// Resolve walks the tag hierarchy and produces a fully-resolved policy
// with source tracking for every field.
//
// Resolution order (highest priority wins):
//  1. Resource-level explicit tags
//  2. Resource-level template
//  3. Resource Group-level tags (if not overridden)
//  4. Resource Group-level template
//  5. Subscription-level tags
//  6. Subscription-level template
//  7. Default
func (r *PolicyResolver) Resolve(h TagHierarchy) *ResolvedPolicy {
	resolved := &ResolvedPolicy{}
	resolved.Override = hasOverride(h.ResourceTags)

	type tagLevel struct {
		tags   map[string]string
		source PolicySource
		name   string
	}

	levels := []tagLevel{
		{h.ResourceTags, SourceResource, ""},
	}
	if !resolved.Override {
		levels = append(levels,
			tagLevel{h.ResourceGroupTags, SourceResourceGroup, h.ResourceGroupName},
			tagLevel{h.SubscriptionTags, SourceSubscription, h.SubscriptionName},
		)
	}

	for _, level := range levels {
		r.resolveLevel(resolved, level.tags, level.source, level.name)
	}

	r.fillDefaults(resolved)

	if !resolved.Override {
		resolved.Drifts = r.detectDrift(h)
	}

	return resolved
}

func (r *PolicyResolver) resolveLevel(resolved *ResolvedPolicy, tags map[string]string, source PolicySource, name string) {
	if tags == nil {
		return
	}

	// Check template first (explicit tags override template)
	tmplName := findTag(tags, []string{"inframind-template", "inframind:template"})
	if tmplName != "" {
		if tmpl, ok := r.templates[strings.ToLower(tmplName)]; ok {
			origin := PolicyOrigin{Source: SourceTemplate, Name: tmplName + " (via " + string(source) + ")"}
			if name != "" {
				origin.Name = tmplName + " (via " + string(source) + " \"" + name + "\")"
			}
			if resolved.ModeOrigin.Source == "" && tmpl.Mode != "" {
				resolved.Mode = tmpl.Mode
				resolved.ModeOrigin = origin
			}
			if resolved.CriticalityOrigin.Source == "" && tmpl.Criticality != "" {
				resolved.Criticality = tmpl.Criticality
				resolved.CriticalityOrigin = origin
			}
			if resolved.ExternalOrigin.Source == "" && tmpl.ExternalDeps {
				resolved.ExternalDeps = true
				resolved.ExternalOrigin = origin
			}
		}
	}

	origin := PolicyOrigin{Source: source, Name: name}

	if resolved.ModeOrigin.Source == "" {
		if m := parseMode(tags); m != ModeDefault {
			resolved.Mode = m
			resolved.ModeOrigin = origin
		}
	}
	if resolved.CriticalityOrigin.Source == "" {
		if c := parseCriticality(tags); c != CriticalityNone {
			resolved.Criticality = c
			resolved.CriticalityOrigin = origin
		}
	}
	if resolved.ExternalOrigin.Source == "" {
		if parseExternal(tags) {
			resolved.ExternalDeps = true
			resolved.ExternalOrigin = origin
		}
	}
}

func (r *PolicyResolver) fillDefaults(resolved *ResolvedPolicy) {
	defaultOrigin := PolicyOrigin{Source: SourceDefault}
	if resolved.ModeOrigin.Source == "" {
		resolved.Mode = ModeDefault
		resolved.ModeOrigin = defaultOrigin
	}
	if resolved.CriticalityOrigin.Source == "" {
		resolved.Criticality = CriticalityNone
		resolved.CriticalityOrigin = defaultOrigin
	}
	if resolved.ExternalOrigin.Source == "" {
		resolved.ExternalDeps = false
		resolved.ExternalOrigin = defaultOrigin
	}
}

// detectDrift compares resource-level values with their parent levels.
// A drift means the resource explicitly diverges from its RG or subscription.
func (r *PolicyResolver) detectDrift(h TagHierarchy) []PolicyDrift {
	var drifts []PolicyDrift

	resMode := parseMode(h.ResourceTags)
	resCrit := parseCriticality(h.ResourceTags)
	resExt := parseExternal(h.ResourceTags)

	type parentLevel struct {
		tags   map[string]string
		source PolicySource
		name   string
	}

	parents := []parentLevel{
		{h.ResourceGroupTags, SourceResourceGroup, h.ResourceGroupName},
		{h.SubscriptionTags, SourceSubscription, h.SubscriptionName},
	}

	for _, parent := range parents {
		if parent.tags == nil {
			continue
		}

		parentMode := r.resolveModeFromTags(parent.tags)
		parentCrit := r.resolveCritFromTags(parent.tags)
		parentExt := r.resolveExtFromTags(parent.tags)

		if resMode != ModeDefault && parentMode != ModeDefault && resMode != parentMode {
			drifts = append(drifts, PolicyDrift{
				Field: "mode", ResourceValue: string(resMode),
				ParentValue: string(parentMode), ParentSource: parent.source, ParentName: parent.name,
			})
		}
		if resCrit != CriticalityNone && parentCrit != CriticalityNone && resCrit != parentCrit {
			drifts = append(drifts, PolicyDrift{
				Field: "criticality", ResourceValue: string(resCrit),
				ParentValue: string(parentCrit), ParentSource: parent.source, ParentName: parent.name,
			})
		}
		if resExt != parentExt && parentExt {
			drifts = append(drifts, PolicyDrift{
				Field: "external", ResourceValue: "false",
				ParentValue: "true", ParentSource: parent.source, ParentName: parent.name,
			})
		}
	}
	return drifts
}

func (r *PolicyResolver) resolveModeFromTags(tags map[string]string) PolicyMode {
	tmplName := findTag(tags, []string{"inframind-template", "inframind:template"})
	if tmplName != "" {
		if tmpl, ok := r.templates[strings.ToLower(tmplName)]; ok && tmpl.Mode != "" {
			return tmpl.Mode
		}
	}
	return parseMode(tags)
}

func (r *PolicyResolver) resolveCritFromTags(tags map[string]string) Criticality {
	tmplName := findTag(tags, []string{"inframind-template", "inframind:template"})
	if tmplName != "" {
		if tmpl, ok := r.templates[strings.ToLower(tmplName)]; ok && tmpl.Criticality != "" {
			return tmpl.Criticality
		}
	}
	return parseCriticality(tags)
}

func (r *PolicyResolver) resolveExtFromTags(tags map[string]string) bool {
	tmplName := findTag(tags, []string{"inframind-template", "inframind:template"})
	if tmplName != "" {
		if tmpl, ok := r.templates[strings.ToLower(tmplName)]; ok && tmpl.ExternalDeps {
			return true
		}
	}
	return parseExternal(tags)
}

func hasOverride(tags map[string]string) bool {
	for _, key := range []string{"inframind-policy", "inframind:policy"} {
		for k, v := range tags {
			if strings.EqualFold(k, key) && strings.ToLower(strings.TrimSpace(v)) == "override" {
				return true
			}
		}
	}
	for _, key := range []string{"inframind-inherit", "inframind:inherit"} {
		for k, v := range tags {
			val := strings.ToLower(strings.TrimSpace(v))
			if strings.EqualFold(k, key) && (val == "false" || val == "no" || val == "none") {
				return true
			}
		}
	}
	return false
}

func findTag(tags map[string]string, keys []string) string {
	for _, key := range keys {
		for k, v := range tags {
			if strings.EqualFold(k, key) {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}
