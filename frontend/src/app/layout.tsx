import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Swarm Control Center | Kiwi",
  description: "Monitor and control your agent swarm",
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
