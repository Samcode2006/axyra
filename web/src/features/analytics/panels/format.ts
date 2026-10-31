/** Shows an id as a short label: cust_000123 becomes cust_0…23. Display only; the full id stays in the API. */
export function maskId(id: string): string {
  return id.length <= 8 ? id : `${id.slice(0, 6)}…${id.slice(-2)}`;
}

/** What every panel receives: the selected range, and the tenant to scope to. */
export interface PanelScope {
  from: string;
  to: string;
  /** Set when the signed-in user chose a tenant. The control plane enforces access regardless. */
  tenantId?: string;
}
