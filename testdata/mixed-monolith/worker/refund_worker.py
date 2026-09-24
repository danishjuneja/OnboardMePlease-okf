"""Synthetic message handler for the refund.requested topic."""


def on_message(topic, order_id, gateway, store, publisher):
    if topic != "refund.requested":
        return
    attempt = store.create_refund_attempt(order_id)
    receipt = gateway.refund(order_id, idempotency_key=attempt.id)
    store.complete_refund_attempt(attempt.id, receipt.id)
    publisher.publish("refund.completed", order_id)
