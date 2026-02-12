package telemetry

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
)

// CostCalculator handles resource and instance tier cost calculations
type CostCalculator struct {
	CPUCostPerHour   float64
	MemoryCostPerGB  float64
	StorageCostPerGB float64
	NetworkCostPerGB float64
	InstanceTiers    []InstanceTier
}

// InstanceTier represents a cloud instance tier configuration
type InstanceTier struct {
	Name        string  `json:"name"`
	CPU         float64 `json:"cpu"`
	MemoryGB    float64 `json:"memory_gb"`
	CostPerHour float64 `json:"cost_per_hour"`
	Type        string  `json:"type"` // burstable, compute_optimized, balanced, memory_optimized
}

// ResourceUsage tracks resource consumption for cost calculation
type ResourceUsage struct {
	CPUCores    float64
	MemoryGB    float64
	StorageGB   float64
	NetworkGB   float64
	DurationHrs float64
}

// CostBreakdown provides detailed cost information
type CostBreakdown struct {
	// Resource-level costs
	CPUCost           float64
	MemoryCost        float64
	StorageCost       float64
	NetworkCost       float64
	TotalResourceCost float64

	// Instance tier costs
	InstanceTier  string
	InstanceHours float64
	InstanceCost  float64
	MatchType     string // "exact" or "rounded_up"
}

var defaultCostCalculator *CostCalculator

// InitCostCalculator initializes the cost calculator from environment variables
func InitCostCalculator() *CostCalculator {
	if defaultCostCalculator != nil {
		return defaultCostCalculator
	}

	calculator := &CostCalculator{
		CPUCostPerHour:   getEnvFloat("COST_CPU_PER_HOUR", 0.04),
		MemoryCostPerGB:  getEnvFloat("COST_MEMORY_PER_GB_HOUR", 0.005),
		StorageCostPerGB: getEnvFloat("COST_STORAGE_PER_GB_HOUR", 0.0001),
		NetworkCostPerGB: getEnvFloat("COST_NETWORK_PER_GB", 0.09),
	}

	// Load instance tier mapping from JSON env var
	instanceTiersJSON := os.Getenv("INSTANCE_TIER_MAPPING")
	if instanceTiersJSON != "" {
		var tiers []InstanceTier
		if err := json.Unmarshal([]byte(instanceTiersJSON), &tiers); err == nil {
			calculator.InstanceTiers = tiers
		}
	}

	// Default instance tiers if not configured
	if len(calculator.InstanceTiers) == 0 {
		calculator.InstanceTiers = []InstanceTier{
			{Name: "burstable_small", CPU: 0.5, MemoryGB: 1, CostPerHour: 0.01, Type: "burstable"},
			{Name: "burstable_medium", CPU: 1.0, MemoryGB: 2, CostPerHour: 0.02, Type: "burstable"},
			{Name: "burstable_large", CPU: 2.0, MemoryGB: 4, CostPerHour: 0.04, Type: "burstable"},
			{Name: "compute_optimized_small", CPU: 2.0, MemoryGB: 2, CostPerHour: 0.05, Type: "compute_optimized"},
			{Name: "compute_optimized_medium", CPU: 4.0, MemoryGB: 4, CostPerHour: 0.10, Type: "compute_optimized"},
			{Name: "balanced_small", CPU: 1.0, MemoryGB: 4, CostPerHour: 0.035, Type: "balanced"},
			{Name: "balanced_medium", CPU: 2.0, MemoryGB: 8, CostPerHour: 0.07, Type: "balanced"},
			{Name: "memory_optimized_small", CPU: 1.0, MemoryGB: 8, CostPerHour: 0.06, Type: "memory_optimized"},
			{Name: "memory_optimized_medium", CPU: 2.0, MemoryGB: 16, CostPerHour: 0.12, Type: "memory_optimized"},
		}
	}

	defaultCostCalculator = calculator
	return calculator
}

// GetCostCalculator returns the singleton cost calculator instance
func GetCostCalculator() *CostCalculator {
	if defaultCostCalculator == nil {
		return InitCostCalculator()
	}
	return defaultCostCalculator
}

// CalculateCosts computes both resource-level and instance-tier costs
func (c *CostCalculator) CalculateCosts(usage ResourceUsage) CostBreakdown {
	breakdown := CostBreakdown{}

	// Resource-level cost calculation
	breakdown.CPUCost = usage.CPUCores * c.CPUCostPerHour * usage.DurationHrs
	breakdown.MemoryCost = usage.MemoryGB * c.MemoryCostPerGB * usage.DurationHrs
	breakdown.StorageCost = usage.StorageGB * c.StorageCostPerGB * usage.DurationHrs
	breakdown.NetworkCost = usage.NetworkGB * c.NetworkCostPerGB
	breakdown.TotalResourceCost = breakdown.CPUCost + breakdown.MemoryCost + breakdown.StorageCost + breakdown.NetworkCost

	// Instance tier matching with round-up logic
	tier, matchType := c.MatchInstanceTier(usage.CPUCores, usage.MemoryGB)
	if tier != nil {
		breakdown.InstanceTier = tier.Name
		breakdown.InstanceHours = usage.DurationHrs
		breakdown.InstanceCost = tier.CostPerHour * usage.DurationHrs
		breakdown.MatchType = matchType
	}

	return breakdown
}

// MatchInstanceTier finds the best matching instance tier using round-up logic
func (c *CostCalculator) MatchInstanceTier(cpuCores, memoryGB float64) (*InstanceTier, string) {
	var bestMatch *InstanceTier
	matchType := "rounded_up"

	// First, try to find an exact match
	for i := range c.InstanceTiers {
		tier := &c.InstanceTiers[i]
		if tier.CPU == cpuCores && tier.MemoryGB == memoryGB {
			return tier, "exact"
		}
	}

	// Round up: find smallest tier that satisfies both CPU and memory requirements
	minDelta := math.MaxFloat64
	for i := range c.InstanceTiers {
		tier := &c.InstanceTiers[i]

		// Tier must meet or exceed both CPU and memory requirements
		if tier.CPU >= cpuCores && tier.MemoryGB >= memoryGB {
			// Calculate "distance" from required resources
			cpuDelta := tier.CPU - cpuCores
			memoryDelta := tier.MemoryGB - memoryGB
			delta := cpuDelta + memoryDelta

			// Select tier with minimum excess capacity
			if delta < minDelta {
				minDelta = delta
				bestMatch = tier
			}
		}
	}

	return bestMatch, matchType
}

func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if floatValue, err := strconv.ParseFloat(value, 64); err == nil {
			return floatValue
		}
	}
	return defaultValue
}
