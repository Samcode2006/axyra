import axios from 'axios';

const BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8000/api/v1';

export const api = axios.create({ baseURL: BASE, timeout: 10000 });

export interface OrderCreate {
  store_id: string;
  amount: number;
  currency?: string;
  product_id: string;
  product_name: string;
  category: string;
}

export interface Order {
  id: string;
  store_id: string;
  amount: number;
  currency: string;
  status: string;
  product_id: string;
  product_name: string;
  category: string;
  created_at: string;
  updated_at: string;
}

export const ordersApi = {
  list: (store_id?: string) =>
    api.get<{ data: Order[]; total: number }>('/orders', { params: store_id ? { store_id } : {} }),
  create: (body: OrderCreate) => api.post<Order>('/orders', body),
  update: (id: string, status: string) => api.patch<Order>(`/orders/${id}`, { status }),
};

