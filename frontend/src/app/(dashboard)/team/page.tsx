"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { client, type AdminOrg } from "@/lib/api";
import { LoadingState } from "@/components/LoadingState";
import { OrgManagementPanel } from "@/components/OrgManagementPanel";

export default function TeamPage() {
  const router = useRouter();
  const [org, setOrg] = useState<AdminOrg | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    client.validate().then(v => {
      if (v.role !== "admin") {
        // Real enforcement is server-side regardless — this just keeps a
        // member from landing on a page that will 403 on every request.
        router.push("/");
        return;
      }
      setOrg({
        id: v.org_id,
        name: v.org_name,
        plan: v.plan,
        activation_state: v.activation_state,
        domain_join: v.domain_join,
        primary_domain: v.primary_domain,
      });
      setLoading(false);
    }).catch(() => {
      router.push("/");
    });
  }, [router]);

  if (loading || !org) {
    return <LoadingState label="Loading team…" className="h-full" />;
  }

  return (
    <div className="p-8 max-w-7xl mx-auto h-full flex flex-col text-white">
      <OrgManagementPanel org={org} onOrgUpdate={setOrg} />
    </div>
  );
}
