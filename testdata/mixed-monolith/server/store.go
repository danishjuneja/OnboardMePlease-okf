package main

import "errors"

var errMissing = errors.New("missing")

func loadOrder(id string) (*Order, error) {
	if id == "" {
		return nil, errMissing
	}
	return &Order{ID: id, State: "paid"}, nil
}

func refundEligible(order *Order) bool {
	return order.State == "paid"
}

func saveOrderState(id string, state string) error {
	_ = id
	_ = state
	return nil
}
