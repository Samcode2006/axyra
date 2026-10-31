package worker

import (
	"testing"
)

func TestDeltaCalculations(t *testing.T) {
	// Case 1: New order placed
	// amount = 1000, status = "placed"
	exists := false
	amount := 1000.0
	status := "placed"

	var deltaTotalOrders int64
	var deltaActiveOrders int64
	var deltaCancelledOrders int64
	var deltaRevenue float64

	if !exists {
		deltaTotalOrders = 1
		if status == "cancelled" {
			deltaCancelledOrders = 1
		} else {
			deltaActiveOrders = 1
			deltaRevenue = amount
		}
	}

	if deltaTotalOrders != 1 || deltaActiveOrders != 1 || deltaRevenue != 1000.0 || deltaCancelledOrders != 0 {
		t.Fatalf("Case 1 failed: got total=%d active=%d rev=%.2f canc=%d", deltaTotalOrders, deltaActiveOrders, deltaRevenue, deltaCancelledOrders)
	}

	// Case 2: Order cancelled
	exists = true
	priorStatus := "placed"
	priorAmount := 1000.0
	status = "cancelled"
	amount = 1000.0

	deltaTotalOrders = 0
	if priorStatus != "cancelled" && status == "cancelled" {
		deltaActiveOrders = -1
		deltaCancelledOrders = 1
		deltaRevenue = -priorAmount
	}

	if deltaTotalOrders != 0 || deltaActiveOrders != -1 || deltaRevenue != -1000.0 || deltaCancelledOrders != 1 {
		t.Fatalf("Case 2 failed: got total=%d active=%d rev=%.2f canc=%d", deltaTotalOrders, deltaActiveOrders, deltaRevenue, deltaCancelledOrders)
	}
}

func TestGetHelpers(t *testing.T) {
	m := map[string]interface{}{
		"str":  "hello",
		"flt":  123.45,
		"int":  42,
		"numS": "99.9",
	}

	if getString(m, "str", "") != "hello" {
		t.Errorf("expected 'hello'")
	}
	if getString(m, "missing", "default") != "default" {
		t.Errorf("expected 'default'")
	}
	if getFloat(m, "flt", 0) != 123.45 {
		t.Errorf("expected 123.45")
	}
	if getFloat(m, "int", 0) != 42.0 {
		t.Errorf("expected 42.0")
	}
	if getFloat(m, "numS", 0) != 99.9 {
		t.Errorf("expected 99.9")
	}
}
