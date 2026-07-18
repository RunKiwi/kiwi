"use client";

import { useFleetStore } from "@/store/useFleetStore";
import { useEffect } from "react";
import { Server, Activity, Clock, ServerCrash } from "lucide-react";

export default function FleetPage() {
  const { daemons, loadDaemons, isLoading } = useFleetStore();
  
  useEffect(() => {
    loadDaemons();
    const interval = setInterval(loadDaemons, 5000); // Poll every 5s for liveness updates
    return () => clearInterval(interval);
  }, [loadDaemons]);
  
  return (
    <div className="p-8 max-w-7xl mx-auto h-full flex flex-col">
      <div className="mb-8">
        <h1 className="text-3xl font-light tracking-tight text-white mb-2">Fleet</h1>
        <p className="text-zinc-400">Monitor your organization&apos;s registered daemons and their liveness.</p>
      </div>
      
      {daemons.length === 0 && !isLoading ? (
        <div className="glass-panel border border-white/5 rounded-2xl p-12 flex flex-col items-center justify-center text-center mt-8">
          <ServerCrash className="w-12 h-12 text-zinc-600 mb-4" />
          <h2 className="text-xl font-medium text-white mb-2">No Daemons Registered</h2>
          <p className="text-zinc-400 max-w-md">
            Generate a join token to register a new BYOC daemon and start executing tasks.
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {daemons.map(d => (
             <div 
               key={d.id} 
               className="glass-panel p-6 border border-white/5 rounded-2xl transition-all duration-300 hover:border-white/10 hover:shadow-[0_0_30px_rgba(255,255,255,0.02)] group flex flex-col"
             >
               <div className="flex items-start justify-between mb-6">
                 <div className="flex items-center gap-3">
                   <div className="w-10 h-10 rounded-xl bg-white/5 border border-white/10 flex items-center justify-center group-hover:bg-white/10 transition-colors">
                     <Server className="w-5 h-5 text-zinc-300" />
                   </div>
                   <div>
                     <h3 className="text-sm font-medium text-white truncate max-w-[150px]" title={d.id}>{d.id}</h3>
                     <div className="flex items-center gap-1.5 mt-1">
                       <span className="relative flex h-2 w-2">
                         {d.online && (
                           <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75"></span>
                         )}
                         <span className={`relative inline-flex rounded-full h-2 w-2 ${d.online ? 'bg-green-500' : 'bg-red-500'}`}></span>
                       </span>
                       <span className="text-xs text-zinc-500 font-medium">
                         {d.online ? 'Online' : 'Offline'}
                       </span>
                     </div>
                   </div>
                 </div>
               </div>
               
               <div className="mt-auto space-y-3 border-t border-white/5 pt-4">
                 <div className="flex items-center justify-between text-xs">
                   <div className="flex items-center gap-1.5 text-zinc-500">
                     <Activity className="w-3.5 h-3.5" />
                     <span>Last Seen</span>
                   </div>
                   <span className="text-zinc-300">
                     {d.last_seen_at ? new Date(d.last_seen_at).toLocaleTimeString() : 'Never'}
                   </span>
                 </div>
                 
                 <div className="flex items-center justify-between text-xs">
                   <div className="flex items-center gap-1.5 text-zinc-500">
                     <Clock className="w-3.5 h-3.5" />
                     <span>Registered</span>
                   </div>
                   <span className="text-zinc-300">
                     {new Date(d.created_at).toLocaleDateString()}
                   </span>
                 </div>
               </div>
             </div>
          ))}
        </div>
      )}
    </div>
  );
}
