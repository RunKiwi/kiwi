"use client";

import { useFleetStore } from "@/store/useFleetStore";
import { useEffect } from "react";

export default function FleetPage() {
  const { daemons, loadDaemons } = useFleetStore();
  
  useEffect(() => {
    loadDaemons();
  }, [loadDaemons]);
  
  return (
    <div className="p-8 max-w-7xl mx-auto h-full flex flex-col text-white">
      <h1 className="text-3xl font-light tracking-tight mb-8">Fleet</h1>
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {daemons.map(d => (
           <div key={d.id} className="glass-panel p-4 border border-white/10 rounded-xl">
             <h3 className="text-lg font-medium mb-2">{d.id}</h3>
             <p className="text-sm text-zinc-400">Online: {d.online ? 'Yes' : 'No'}</p>
           </div>
        ))}
      </div>
    </div>
  );
}
