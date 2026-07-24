export interface PlanFeatureValue {
  value: string | boolean;
  soon?: boolean;
}

export interface PlanFeature {
  name: string;
  free: PlanFeatureValue;
  pro: PlanFeatureValue;
  enterprise: PlanFeatureValue;
}

export interface PlanTier {
  id: "free" | "pro" | "enterprise";
  name: string;
  price: string;
}

export const PLAN_TIERS: PlanTier[] = [
  { id: "free", name: "Free", price: "$0" },
  { id: "pro", name: "Pro", price: "$18/user/mo + $10 per 250 extra min" },
  { id: "enterprise", name: "Enterprise", price: "Custom" },
];

export const PLAN_FEATURES: PlanFeature[] = [
  {
    name: "Agent-minutes/mo",
    free: { value: "200" },
    pro: { value: "2000 / seat (pooled) + buy more" },
    enterprise: { value: "Custom" },
  },
  {
    name: "Concurrent jobs",
    free: { value: "1" },
    pro: { value: "20" },
    enterprise: { value: "Custom" },
  },
  {
    name: "Fleet",
    free: { value: "Shared managed" },
    pro: { value: "Dedicated managed", soon: true },
    enterprise: { value: "BYOC or dedicated" },
  },
  {
    name: "Swarm width",
    free: { value: "Up to 4" },
    pro: { value: "Higher" },
    enterprise: { value: "Custom" },
  },
  {
    name: "GitHub",
    free: { value: true },
    pro: { value: true },
    enterprise: { value: true },
  },
  {
    name: "Linear",
    free: { value: false },
    pro: { value: true },
    enterprise: { value: true },
  },
  {
    name: "Slack",
    free: { value: false },
    pro: { value: true, soon: true },
    enterprise: { value: true, soon: true },
  },
  {
    name: "gVisor sandbox + credential sealing",
    free: { value: true },
    pro: { value: true },
    enterprise: { value: true },
  },
  {
    name: "Bring-your-own model key",
    free: { value: true },
    pro: { value: true },
    enterprise: { value: true },
  },
  {
    name: "Shared context",
    free: { value: true },
    pro: { value: true },
    enterprise: { value: true },
  },
  {
    name: "Run in your own cloud (BYOC, zero-knowledge)",
    free: { value: false },
    pro: { value: true },
    enterprise: { value: true },
  },
  {
    name: "Firecracker microVM",
    free: { value: false },
    pro: { value: false },
    enterprise: { value: true, soon: true },
  },
  {
    name: "Data residency / on-prem",
    free: { value: false },
    pro: { value: false },
    enterprise: { value: true },
  },
  {
    name: "Domain team join",
    free: { value: false },
    pro: { value: true },
    enterprise: { value: true },
  },
  {
    name: "SSO / SAML",
    free: { value: false },
    pro: { value: false },
    enterprise: { value: true, soon: true },
  },
  {
    name: "Advanced RBAC",
    free: { value: false },
    pro: { value: false },
    enterprise: { value: true, soon: true },
  },
  {
    name: "Audit logs",
    free: { value: false },
    pro: { value: false },
    enterprise: { value: true, soon: true },
  },
  {
    name: "Compliance (SOC2…)",
    free: { value: false },
    pro: { value: false },
    enterprise: { value: "On request" },
  },
  {
    name: "Support",
    free: { value: "Community" },
    pro: { value: "Priority email" },
    enterprise: { value: "Dedicated + SLA" },
  },
];
