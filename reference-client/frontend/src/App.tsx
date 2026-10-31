import { useState, useEffect } from 'react';
import { ordersApi, type Order, type OrderCreate } from './api';
import './App.css';

const STORES = ['store_001', 'store_002'];
const PRODUCTS = [
  { id: 'prod_001', name: 'Wireless Headphones', category: 'Electronics' },
  { id: 'prod_002', name: 'Running Shoes', category: 'Footwear' },
  { id: 'prod_003', name: 'Coffee Maker', category: 'Appliances' },
  { id: 'prod_004', name: 'Yoga Mat', category: 'Fitness' },
  { id: 'prod_005', name: 'Backpack', category: 'Accessories' },
];

const AXYRA_URL = import.meta.env.VITE_AXYRA_URL ?? 'http://localhost:5173';

const STATUS_COLORS: Record<string, string> = {
  placed: '#6c63ff',
  confirmed: '#3b82f6',
  shipped: '#f59e0b',
  delivered: '#22c55e',
  cancelled: '#ef4444',
};

function App() {
  const [orders, setOrders] = useState<Order[]>([]);
  const [filterStore, setFilterStore] = useState('');
  const [loading, setLoading] = useState(false);
  const [form, setForm] = useState<Partial<OrderCreate>>({ currency: 'INR', store_id: STORES[0] });
  const [submitting, setSubmitting] = useState(false);
  const [showForm, setShowForm] = useState(false);

  const load = () => {
    setLoading(true);
    ordersApi
      .list(filterStore || undefined)
      .then((r) => setOrders(r.data.data))
      .finally(() => setLoading(false));
  };

  useEffect(load, [filterStore]);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!form.store_id || !form.product_id || !form.amount) return;
    setSubmitting(true);
    try {
      const product = PRODUCTS.find((p) => p.id === form.product_id)!;
      await ordersApi.create({
        store_id: form.store_id!,
        amount: Number(form.amount),
        currency: form.currency ?? 'INR',
        product_id: product.id,
        product_name: product.name,
        category: product.category,
      });
      setShowForm(false);
      load();
    } finally {
      setSubmitting(false);
    }
  };

  const handleCancel = async (id: string) => {
    if (!confirm('Cancel this order?')) return;
    await ordersApi.update(id, 'cancelled');
    load();
  };

  return (
    <div className="app">
      <header className="header">
        <div className="header-brand">
          <span className="brand-dot" />
          <h1>Demo Store</h1>
        </div>
        <a href={AXYRA_URL} target="_blank" rel="noreferrer" className="btn-axyra">
          Open Axyra Console ↗
        </a>
      </header>

      <main className="main">
        <div className="toolbar">
          <div className="toolbar-left">
            <h2>Orders</h2>
            <select value={filterStore} onChange={(e) => setFilterStore(e.target.value)} className="select">
              <option value="">All stores</option>
              {STORES.map((s) => <option key={s} value={s}>{s}</option>)}
            </select>
          </div>
          <button className="btn-primary" onClick={() => setShowForm(!showForm)}>
            {showForm ? 'Cancel' : '+ New Order'}
          </button>
        </div>

        {showForm && (
          <form className="order-form" onSubmit={handleCreate}>
            <h3>Create Order</h3>
            <div className="form-grid">
              <label>Store (Tenant)
                <select required value={form.store_id} onChange={(e) => setForm({ ...form, store_id: e.target.value })}>
                  {STORES.map((s) => <option key={s} value={s}>{s}</option>)}
                </select>
              </label>
              <label>Product
                <select required value={form.product_id ?? ''} onChange={(e) => setForm({ ...form, product_id: e.target.value })}>
                  <option value="">Select…</option>
                  {PRODUCTS.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
                </select>
              </label>
              <label>Amount (INR)
                <input type="number" required min="1" value={form.amount ?? ''} onChange={(e) => setForm({ ...form, amount: Number(e.target.value) })} placeholder="1299" />
              </label>
            </div>
            <button type="submit" className="btn-primary" disabled={submitting}>
              {submitting ? 'Placing…' : 'Place Order'}
            </button>
            <p className="form-note">
              ℹ️ No Axyra API call is made. The Axyra Agent captures this INSERT from the PostgreSQL WAL automatically.
            </p>
          </form>
        )}

        {loading ? (
          <div className="loading">Loading…</div>
        ) : orders.length === 0 ? (
          <div className="empty">No orders yet. Create one to see the Axyra Agent in action.</div>
        ) : (
          <div className="order-list">
            {orders.map((o) => (
              <div key={o.id} className="order-card">
                <div className="order-header">
                  <span className="order-id">{o.id}</span>
                  <span className="status-pill" style={{ background: STATUS_COLORS[o.status] + '22', color: STATUS_COLORS[o.status] }}>
                    {o.status}
                  </span>
                </div>
                <div className="order-body">
                  <span className="order-product">{o.product_name}</span>
                  <span className="order-cat">{o.category}</span>
                </div>
                <div className="order-footer">
                  <span className="order-store">🏪 {o.store_id}</span>
                  <span className="order-amount">₹{o.amount.toLocaleString('en-IN')}</span>
                  {o.status !== 'cancelled' && (
                    <button className="btn-cancel" onClick={() => handleCancel(o.id)}>Cancel</button>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </main>
    </div>
  );
}

export default App;
