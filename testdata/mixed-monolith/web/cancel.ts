type CancelResult = { status: "accepted"; orderId: string };

export async function cancelPaidOrder(orderId: string): Promise<CancelResult> {
  const response = await fetch(`/api/orders/${encodeURIComponent(orderId)}/cancel`, {
    method: "POST",
    credentials: "same-origin",
  });
  if (!response.ok) {
    throw new Error(`Cancellation failed: ${response.status}`);
  }
  return response.json() as Promise<CancelResult>;
}
