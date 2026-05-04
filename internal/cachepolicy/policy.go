package cachepolicy

import "time"

type CleanupMode string

const (
	CleanupModeBackground CleanupMode = "background"
	CleanupModeOnDemand   CleanupMode = "on-demand"
	CleanupModeOff        CleanupMode = "off"
)

type RetentionClass string

const (
	RetentionClassWorktreeEphemeral RetentionClass = "worktree-ephemeral"
	RetentionClassMachineShareable  RetentionClass = "machine-shareable"
	RetentionClassRemoteShareable   RetentionClass = "remote-shareable"
	RetentionClassDiagnostic        RetentionClass = "diagnostic"
	RetentionClassIndex             RetentionClass = "index"
	RetentionClassPinned            RetentionClass = "pinned"
)

type Shareability string

const (
	ShareabilityWorktreeOnly Shareability = "worktree-only"
	ShareabilityMachine      Shareability = "machine"
	ShareabilityRemote       Shareability = "remote"
)

type CleanupDisposition string

const (
	CleanupDispositionProtected CleanupDisposition = "protected"
	CleanupDispositionEvictable CleanupDisposition = "evictable"
)

type CleanupReasonCode string

const (
	CleanupReasonCurrentSemanticSummary             CleanupReasonCode = "current-semantic-summary"
	CleanupReasonNotCurrentSemanticSummary          CleanupReasonCode = "not-current-semantic-summary"
	CleanupReasonReachableMaterializationSummary    CleanupReasonCode = "reachable-materialization-summary"
	CleanupReasonNotReachableMaterializationSummary CleanupReasonCode = "not-reachable-materialization-summary"
	CleanupReasonCurrentMaterializationRoot         CleanupReasonCode = "current-materialization-root"
	CleanupReasonNotCurrentMaterializationRoot      CleanupReasonCode = "not-current-materialization-root"
)

type CleanupWarningScope string

const (
	CleanupWarningScopePlan  CleanupWarningScope = "plan"
	CleanupWarningScopeClass CleanupWarningScope = "class"
)

type CleanupWarningKind string

const (
	CleanupWarningUnknownSize          CleanupWarningKind = "unknown-size"
	CleanupWarningTargetBudgetExceeded CleanupWarningKind = "target-budget-exceeded"
	CleanupWarningHardBudgetExceeded   CleanupWarningKind = "hard-budget-exceeded"
	CleanupWarningSharedTargetExceeded CleanupWarningKind = "shared-target-budget-exceeded"
	CleanupWarningSharedHardExceeded   CleanupWarningKind = "shared-hard-budget-exceeded"
)

type CleanupWarning struct {
	Scope            CleanupWarningScope `json:"scope"`
	Class            RetentionClass      `json:"class,omitempty"`
	Kind             CleanupWarningKind  `json:"kind"`
	ObservedBytes    int64               `json:"observedBytes,omitempty"`
	LimitBytes       int64               `json:"limitBytes,omitempty"`
	UnknownSizeCount int                 `json:"unknownSizeCount,omitempty"`
	Message          string              `json:"message,omitempty"`
}

type CleanupRecord struct {
	Kind           string             `json:"kind"`
	ID             string             `json:"id"`
	ModulePath     string             `json:"modulePath,omitempty"`
	VariantName    string             `json:"variantName,omitempty"`
	Path           string             `json:"path,omitempty"`
	PathExists     *bool              `json:"pathExists,omitempty"`
	SizeBytes      int64              `json:"sizeBytes,omitempty"`
	ModifiedAt     *time.Time         `json:"modifiedAt,omitempty"`
	AgeHours       float64            `json:"ageHours,omitempty"`
	CacheKey       string             `json:"cacheKey,omitempty"`
	Disposition    CleanupDisposition `json:"disposition"`
	ReasonCode     CleanupReasonCode  `json:"reasonCode,omitempty"`
	Reason         string             `json:"reason,omitempty"`
	RetentionClass RetentionClass     `json:"retentionClass,omitempty"`
	Shareability   Shareability       `json:"shareability,omitempty"`
}

type CleanupClassPlan struct {
	Class            RetentionClass   `json:"class"`
	Shareability     Shareability     `json:"shareability,omitempty"`
	Policy           ClassPolicy      `json:"policy"`
	RecordCount      int              `json:"recordCount"`
	ProtectedCount   int              `json:"protectedCount"`
	EvictableCount   int              `json:"evictableCount"`
	UnknownSizeCount int              `json:"unknownSizeCount"`
	TotalBytes       int64            `json:"totalBytes,omitempty"`
	ProtectedBytes   int64            `json:"protectedBytes,omitempty"`
	EvictableBytes   int64            `json:"evictableBytes,omitempty"`
	RecordsByKind    map[string]int   `json:"recordsByKind,omitempty"`
	Warnings         []CleanupWarning `json:"warnings,omitempty"`
	Notes            []string         `json:"notes,omitempty"`
}

type CleanupPlan struct {
	ModelCacheKey  string                     `json:"modelCacheKey,omitempty"`
	Policy         Policy                     `json:"policy"`
	ClassPlans     []CleanupClassPlan         `json:"classPlans,omitempty"`
	Records        []CleanupRecord            `json:"records,omitempty"`
	Notes          []string                   `json:"notes,omitempty"`
	Totals         map[CleanupDisposition]int `json:"totals,omitempty"`
	KnownBytes     int64                      `json:"knownBytes,omitempty"`
	ProtectedBytes int64                      `json:"protectedBytes,omitempty"`
	EvictableBytes int64                      `json:"evictableBytes,omitempty"`
	Warnings       []CleanupWarning           `json:"warnings,omitempty"`
}

type ClassPolicy struct {
	Class                 RetentionClass `json:"class"`
	MaxAge                time.Duration  `json:"maxAge,omitempty"`
	TargetBytes           int64          `json:"targetBytes,omitempty"`
	HardBytes             int64          `json:"hardBytes,omitempty"`
	EvictionOrder         int            `json:"evictionOrder,omitempty"`
	Shareability          Shareability   `json:"shareability,omitempty"`
	RequiresReachableRoot bool           `json:"requiresReachableRoot,omitempty"`
}

type Policy struct {
	CleanupMode   CleanupMode                    `json:"cleanupMode"`
	SharedTarget  int64                          `json:"sharedTargetBytes,omitempty"`
	SharedHard    int64                          `json:"sharedHardBytes,omitempty"`
	ClassPolicies map[RetentionClass]ClassPolicy `json:"classPolicies,omitempty"`
}

func DefaultPolicy() Policy {
	return Policy{
		CleanupMode:  CleanupModeBackground,
		SharedTarget: 20 << 30,
		SharedHard:   24 << 30,
		ClassPolicies: map[RetentionClass]ClassPolicy{
			RetentionClassWorktreeEphemeral: {
				Class:                 RetentionClassWorktreeEphemeral,
				MaxAge:                7 * 24 * time.Hour,
				EvictionOrder:         10,
				Shareability:          ShareabilityWorktreeOnly,
				RequiresReachableRoot: false,
			},
			RetentionClassMachineShareable: {
				Class:                 RetentionClassMachineShareable,
				MaxAge:                30 * 24 * time.Hour,
				EvictionOrder:         50,
				Shareability:          ShareabilityMachine,
				RequiresReachableRoot: false,
			},
			RetentionClassRemoteShareable: {
				Class:                 RetentionClassRemoteShareable,
				MaxAge:                30 * 24 * time.Hour,
				EvictionOrder:         60,
				Shareability:          ShareabilityRemote,
				RequiresReachableRoot: false,
			},
			RetentionClassDiagnostic: {
				Class:                 RetentionClassDiagnostic,
				MaxAge:                7 * 24 * time.Hour,
				EvictionOrder:         5,
				Shareability:          ShareabilityMachine,
				RequiresReachableRoot: false,
			},
			RetentionClassIndex: {
				Class:                 RetentionClassIndex,
				MaxAge:                7 * 24 * time.Hour,
				EvictionOrder:         6,
				Shareability:          ShareabilityMachine,
				RequiresReachableRoot: false,
			},
			RetentionClassPinned: {
				Class:                 RetentionClassPinned,
				EvictionOrder:         1000,
				Shareability:          ShareabilityMachine,
				RequiresReachableRoot: true,
			},
		},
	}
}
