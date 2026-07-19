import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Kiwi Dashboard",
  description: "Plan tasks, run agent fleets, and ship pull requests with Kiwi.",
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
