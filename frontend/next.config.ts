import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Emit a self-contained server (.next/standalone) so the Docker run stage
  // ships only the traced runtime deps instead of the full node_modules.
  output: "standalone",
};

export default nextConfig;
