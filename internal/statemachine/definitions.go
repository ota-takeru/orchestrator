package statemachine

var Task = New("task", map[string][]string{
	"proposed":               {"ready", "needs_decision", "cancelled"},
	"ready":                  {"implementing", "needs_input", "needs_decision", "cancelled"},
	"implementing":           {"verifying", "repairing", "needs_input", "needs_decision", "blocked_on_policy", "failed"},
	"verifying":              {"reviewing", "diagnosing", "blocked_on_environment", "failed"},
	"diagnosing":             {"repairing", "ready", "proposed", "blocked_on_environment", "needs_decision", "failed"},
	"repairing":              {"verifying", "diagnosing", "needs_decision", "blocked_on_policy"},
	"reviewing":              {"ready_for_human_review", "repairing", "proposed", "needs_input", "needs_decision", "blocked_on_policy"},
	"needs_input":            {"ready", "verifying", "cancelled"},
	"blocked_on_environment": {"ready", "verifying", "cancelled"},
	"needs_decision":         {"ready", "repairing", "reviewing", "manually_applied", "cancelled"},
	"blocked_on_policy":      {"cancelled", "proposed"},
	"ready_for_human_review": {"approved_for_merge", "repairing", "needs_decision"},
	"approved_for_merge":     {"queued_for_merge", "patch_exported", "ready_for_human_review"},
	"queued_for_merge":       {"rebasing", "cancelled"},
	"rebasing":               {"reverifying", "merge_conflict", "blocked_on_policy"},
	"merge_conflict":         {"rebasing", "needs_decision", "cancelled"},
	"reverifying":            {"merged", "applied", "repairing", "blocked_on_environment", "needs_decision", "blocked_on_policy"},
	"patch_exported":         {"manually_applied", "ready_for_human_review", "cancelled"},
	"manually_applied":       {"reverifying", "needs_decision"},
}, []string{"merged", "applied", "failed", "cancelled"})

var Run = New("run", map[string][]string{
	"pending": {"running", "cancelled"},
	"running": {"succeeded", "failed", "cancelled", "timed_out", "blocked"},
	"blocked": {"cancelled"},
}, []string{"succeeded", "failed", "cancelled", "timed_out"})

var CommandEvent = New("command_event", map[string][]string{
	"pending": {"running", "cancelled"},
	"running": {"succeeded", "failed", "timed_out", "blocked", "cancelled"},
	"blocked": {"cancelled"},
}, []string{"succeeded", "failed", "timed_out", "cancelled"})

var ExecutionEnvironment = New("execution_environment", map[string][]string{
	"detected":   {"configured", "invalid", "disabled"},
	"configured": {"checking", "ready", "invalid", "disabled"},
	"checking":   {"ready", "missing", "invalid", "disabled"},
	"ready":      {"checking", "invalid", "disabled"},
	"missing":    {"configured", "disabled"},
	"invalid":    {"checking", "configured", "disabled"},
	"disabled":   {"configured"},
}, nil)

var RunProfile = New("run_profile", map[string][]string{
	"draft":    {"active", "invalid", "disabled"},
	"active":   {"invalid", "disabled"},
	"invalid":  {"active", "disabled"},
	"disabled": {"active"},
}, nil)

var PathMapping = New("path_mapping", map[string][]string{
	"active":   {"invalid", "disabled"},
	"invalid":  {"active", "disabled"},
	"disabled": {"active"},
}, nil)

var TargetPlatform = New("target_platform", map[string][]string{
	"draft":       {"active", "unsupported", "disabled"},
	"active":      {"unsupported", "disabled"},
	"unsupported": {"active", "disabled"},
	"disabled":    {"active"},
}, nil)

var ToolchainRequirement = New("toolchain_requirement", map[string][]string{
	"missing":        {"detected", "setup_required", "waived", "unsupported"},
	"setup_required": {"detected", "waived", "unsupported"},
	"invalid":        {"detected", "setup_required", "waived", "unsupported"},
	"detected":       {"invalid", "revoked"},
	"waived":         {"missing", "detected", "revoked"},
	"unsupported":    {"missing", "revoked"},
	"revoked":        {"missing"},
}, []string{"revoked"})

var ProjectLifecycle = New("project_lifecycle", map[string][]string{
	"concept":       {"spec_ready", "blocked"},
	"spec_ready":    {"roadmap_ready", "blocked"},
	"roadmap_ready": {"implementing", "blocked"},
	"implementing":  {"blocked", "complete"},
	"blocked":       {"spec_ready", "roadmap_ready", "implementing", "complete"},
	"complete":      {"implementing"},
}, nil)

var ArtifactVersion = New("artifact_version", map[string][]string{
	"draft":               {"proposed", "rejected", "superseded"},
	"proposed":            {"approved", "approved_with_notes", "rejected", "superseded"},
	"approved":            {"superseded"},
	"approved_with_notes": {"superseded"},
	"rejected":            {"superseded"},
}, []string{"superseded"})

var Artifact = New("artifact", map[string][]string{
	"draft":               {"proposed", "superseded"},
	"proposed":            {"approved", "approved_with_notes", "rejected", "superseded"},
	"approved":            {"superseded", "proposed"},
	"approved_with_notes": {"superseded", "proposed"},
	"rejected":            {"proposed", "superseded"},
}, []string{"superseded"})

var HumanApproval = New("human_approval", map[string][]string{
	"open":     {"approved", "rejected", "revised", "cancelled"},
	"approved": {"revoked"},
	"rejected": {"open"},
	"revised":  {"open"},
}, []string{"revoked", "cancelled"})

var MergeQueue = New("merge_queue", map[string][]string{
	"queued":         {"rebasing", "cancelled"},
	"rebasing":       {"reverifying", "merge_conflict", "cancelled"},
	"merge_conflict": {"rebasing", "cancelled"},
	"reverifying":    {"merged", "merge_conflict", "cancelled"},
}, []string{"merged", "cancelled"})

var PatchApplication = New("patch_application", map[string][]string{
	"exported":         {"manually_applied", "cancelled"},
	"manually_applied": {"verifying"},
	"verifying":        {"verified", "needs_decision", "failed"},
	"needs_decision":   {"manually_applied", "verifying", "cancelled", "failed"},
}, []string{"verified", "cancelled", "failed"})
