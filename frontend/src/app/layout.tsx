import type { Metadata } from "next";
import { Inter, JetBrains_Mono } from "next/font/google";
import "./globals.css";

// The theme references --font-geist-sans/mono; wire them to real faces so the UI
// isn't falling back to Arial (Inter for text, JetBrains Mono for ids/code).
const sans = Inter({ subsets: ["latin"], variable: "--font-geist-sans", display: "swap" });
const mono = JetBrains_Mono({ subsets: ["latin"], variable: "--font-geist-mono", display: "swap" });

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
    <html lang="en" className={`dark ${sans.variable} ${mono.variable}`}>
      <body className="antialiased min-h-screen">
        {children}
      </body>
    </html>
  );
}
