package main

import (
	"encoding/json"
	"net/http"
)

type Order struct {
	ID           string
	State        string
	AuditPending bool
}

type Publisher interface {
	Publish(topic string, orderID string) error
}

var publisher Publisher

func cancelHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	order, err := loadOrder(id)
	if err != nil {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}
	if order.State != "paid" {
		http.Error(w, "order is not paid", http.StatusConflict)
		return
	}
	if !refundEligible(order) {
		http.Error(w, "refund unavailable", http.StatusConflict)
		return
	}
	if err := processCancellation(order, false); err != nil {
		http.Error(w, "cancellation failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "accepted", "orderId": id})
}

func processCancellation(order *Order, replayed bool) error {
	if !replayed {
		if err := saveOrderState(order.ID, "cancellation_pending"); err != nil {
			return err
		}
		if err := publisher.Publish("refund.requested", order.ID); err != nil {
			return err
		}
	}
	return auditCancellation(order, replayed)
}

func auditCancellation(order *Order, replayed bool) error {
	if order.AuditPending && !replayed {
		return processCancellation(order, true)
	}
	return nil
}
