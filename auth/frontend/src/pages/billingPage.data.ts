// This file contains mock data for the billing page.
// In a real application, this data would likely come from a combination of
// API calls and static configuration.

export const billingData = {
  title: "Billing",
  subscribeButtonText: "Upgrade to Pro",
  plans: {
    free: {
      title: "Hobby",
      price: "Free",
      features: [
        "Up to 10k vectors",
        "Community Support",
        "Shared Infrastructure",
        "30-day log retention",
      ],
      usage: {
        vectors: { used: 8500, limit: "10,000", unit: "vectors" },
        reads: { used: 45200, limit: "50,000", unit: "req/mo" },
      },
    },
    pro: {
      title: "Pro",
      price: "$29",
      features: [
        "Up to 10M vectors",
        "Priority Email Support",
        "Dedicated Processing Units",
        "Unlimited log retention",
        "RBAC & SSO",
      ],
      usage: {
        vectors: { used: 124500, limit: "10,000,000", unit: "vectors" },
        reads: { used: 45200, limit: "Unlimited", unit: "req/mo" },
      },
    },
  },
  invoiceHistory: [
    {
      date: "Jan 1, 2026",
      amount: "$29.00",
      status: "Paid",
    },
    {
      date: "Dec 1, 2025",
      amount: "$29.00",
      status: "Paid",
    },
    {
      date: "Nov 1, 2025",
      amount: "$29.00",
      status: "Paid",
    },
  ],
};