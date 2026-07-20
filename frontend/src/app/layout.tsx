import type { Metadata } from "next";
import "./globals.css";

const TITLE = "Kiwi — One issue in. One PR out.";
const DESCRIPTION =
  "Plan a task, run a swarm of agents, and ship one verified pull request. The Kiwi dashboard.";

export const metadata: Metadata = {
  metadataBase: new URL("https://app.runkiwi.dev"),
  title: "Kiwi Dashboard",
  description: DESCRIPTION,
  applicationName: "Kiwi",
  // Use the kiwi-bird mark (app/icon.svg) — the default create-next-app
  // favicon.ico was removed so it can't be picked up in link unfurls.
  openGraph: {
    type: "website",
    siteName: "Kiwi",
    url: "https://app.runkiwi.dev",
    title: TITLE,
    description: DESCRIPTION,
  },
  twitter: {
    card: "summary_large_image",
    title: TITLE,
    description: DESCRIPTION,
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" className="dark">
      <body className="antialiased min-h-screen">
        {children}
      </body>
    </html>
  );
}
