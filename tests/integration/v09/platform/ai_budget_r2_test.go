//go:build cx_r2_controller

package platform_test

import "testing"

func TestR2ControllerBudgetHTTPCurrentOwnerCASAuditAndGeneration(t *testing.T) {
	runControllerHTTPBorrowedBYOKBudget(t, true)
}
