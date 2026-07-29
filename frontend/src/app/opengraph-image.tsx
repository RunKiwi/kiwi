import { ImageResponse } from "next/og";
import { KIWI_MARK_PATH } from "@/components/Logo";

// Branded social-card image for link unfurls (Slack, Twitter, etc.). Next wires
// this to og:image and twitter:image automatically.
export const alt = "Kiwi — One issue in. One PR out.";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

export default function OpengraphImage() {
  return new ImageResponse(
    (
      <div
        style={{
          height: "100%",
          width: "100%",
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
          background: "#0A1017",
          padding: "72px",
          color: "#EAF0F2",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: "28px" }}>
          <div
            style={{
              display: "flex",
              width: "104px",
              height: "104px",
              alignItems: "center",
              justifyContent: "center",
              background: "#0E1A24",
              borderRadius: "24px",
              boxShadow: "0 0 48px rgba(147,198,69,0.35)",
            }}
          >
            {/* Kiwi mark, inline so Satori renders it natively. The shared path
                punches the eye with fill-rule evenodd; Satori honours it, and
                unlike a <mask> — which the rasterizer rejects — it needs no
                overpainted dot in the tile colour to fake the counter. */}
            <svg width={76} height={76} viewBox="0 0 128 128" fill="#93C645" fillRule="evenodd">
              <path d={KIWI_MARK_PATH} />
            </svg>
          </div>
          <div style={{ fontSize: "56px", fontWeight: 700, letterSpacing: "-1px" }}>Kiwi</div>
        </div>

        <div style={{ display: "flex", flexDirection: "column", gap: "18px" }}>
          <div style={{ fontSize: "82px", fontWeight: 700, lineHeight: 1.02, letterSpacing: "-2px" }}>
            One issue in. One PR out.
          </div>
          <div style={{ fontSize: "34px", color: "#9DB0BC", lineHeight: 1.3 }}>
            Plan a task, run a swarm of agents, ship one verified pull request.
          </div>
        </div>

        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", fontSize: "28px" }}>
          <div style={{ display: "flex", color: "#93C645" }}>app.runkiwi.dev</div>
          <div style={{ display: "flex", color: "#6E8290" }}>Managed · or bring your own cloud</div>
        </div>
      </div>
    ),
    { ...size },
  );
}
