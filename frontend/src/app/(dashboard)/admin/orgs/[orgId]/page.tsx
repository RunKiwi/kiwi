"use client";

import { useEffect, useState, use } from "react";
import { useRouter } from "next/navigation";
import { client, type AdminOrg } from "@/lib/api";
import { ArrowLeft } from "lucide-react";
import { LoadingState } from "@/components/LoadingState";
import { OrgManagementPanel } from "@/components/OrgManagementPanel";
import Link from "next/link";

export default function AdminOrgPage({ params }: { params: Promise<{ orgId: string }> }) {
  const router = useRouter();
  const unwrappedParams = use(params);
  const orgId = unwrappedParams.orgId;

  const [org, setOrg] = useState<AdminOrg | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    client.getUsage().then(u => {
      if (!u.is_super_admin) {
        router.push("/");
        return;
      }
      return client.listAdminOrgs().then(orgs => orgs.find(o => o.id === orgId) || null).then(o => {
        if (!o) {
          router.push("/admin");
          return;
        }
        setOrg(o);
        setLoading(false);
      });
    }).catch(() => {
      router.push("/");
    });
  }, [router, orgId]);

  if (loading || !org) {
    return <LoadingState label="Loading org details…" className="h-full" />;
  }

  return (
    <div className="p-8 max-w-7xl mx-auto h-full flex flex-col">
      <Link href="/admin" className="text-sm text-stone-500 hover:text-stone-900 flex items-center gap-1 mb-4 w-fit">
        <ArrowLeft className="w-4 h-4" /> Back to Admin
      </Link>
      <OrgManagementPanel org={org} onOrgUpdate={setOrg} />
    </div>
  );
}
