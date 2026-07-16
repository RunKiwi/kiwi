import { create } from 'zustand';

export interface Node {
  id: string;
  provider: 'AWS' | 'GCP';
  status: 'polling' | 'disconnected' | 'executing';
  lastSeen: Date;
}

export interface SubAgent {
  id: string;
  nodeId: string;
  role: 'master' | 'worker';
  phase: 'planning' | 'executing' | 'completed' | 'failed';
  title: string;
}

export interface PullRequest {
  id: string;
  status: 'open' | 'merged';
}

export interface Task {
  id: string;
  phase: 'planning' | 'executing' | 'completed' | 'failed';
  title: string;
  startedAt: Date;
  subAgents: SubAgent[];
  pullRequests: PullRequest[];
}

interface FleetState {
  nodes: Node[];
  tasks: Task[];
  addNode: (node: Node) => void;
  updateNodeStatus: (id: string, status: Node['status']) => void;
  addTask: (task: Task) => void;
  updateTaskPhase: (id: string, phase: Task['phase']) => void;
}

export const useFleetStore = create<FleetState>((set) => ({
  nodes: [
    { id: 'aws-node-01', provider: 'AWS', status: 'executing', lastSeen: new Date() },
    { id: 'aws-node-02', provider: 'AWS', status: 'executing', lastSeen: new Date() },
    { id: 'gcp-node-01', provider: 'GCP', status: 'polling', lastSeen: new Date() },
    { id: 'gcp-node-02', provider: 'GCP', status: 'disconnected', lastSeen: new Date() },
  ],
  tasks: Array.from({ length: 8 }).map((_, i) => {
    const isCompleted = i < 2;
    const isPlanning = i > 5;
    const phase = isCompleted ? 'completed' : isPlanning ? 'planning' : 'executing';
    
    const subAgents: SubAgent[] = [
      {
        id: `agent-${i}-master`,
        nodeId: 'aws-node-01',
        role: 'master',
        phase: phase === 'planning' ? 'executing' : 'completed',
        title: 'Fable Planner'
      },
      {
        id: `agent-${i}-w1`,
        nodeId: 'aws-node-02',
        role: 'worker',
        phase: phase === 'completed' ? 'completed' : phase === 'planning' ? 'planning' : 'executing',
        title: 'Execution Worker 1'
      },
      {
        id: `agent-${i}-w2`,
        nodeId: 'gcp-node-01',
        role: 'worker',
        phase: phase === 'completed' ? 'completed' : 'planning',
        title: 'Execution Worker 2'
      }
    ];

    let prs: { id: string, status: 'open' | 'merged' }[] = [];
    if (i === 1) prs = [{ id: `pr-1a`, status: 'merged' }, { id: `pr-1b`, status: 'merged' }]; // Multiple closed
    else if (i === 3) prs = [{ id: `pr-3a`, status: 'open' }, { id: `pr-3b`, status: 'open' }, { id: `pr-3c`, status: 'open' }]; // Multiple open
    else if (i === 5) prs = [{ id: `pr-5a`, status: 'merged' }, { id: `pr-5b`, status: 'open' }]; // Mixed
    else if (i === 7) prs = [{ id: `pr-7`, status: 'open' }]; // Single open

    return {
      id: `task-${1000 + i}`,
      phase,
      title: `Goal: ${['Deploy Microservices', 'Run Security Audit', 'Provision EKS Cluster', 'Migrate Database', 'Run CI/CD Pipeline', 'Backup Vault', 'Scale Up Workers', 'Update Certificates'][i]}`,
      startedAt: new Date(Date.now() - Math.random() * 1000000),
      subAgents,
      pullRequests: prs
    };
  }),
  
  addNode: (node) => set((state) => ({ nodes: [...state.nodes, node] })),
  updateNodeStatus: (id, status) => set((state) => ({
    nodes: state.nodes.map(n => n.id === id ? { ...n, status, lastSeen: new Date() } : n)
  })),
  
  addTask: (task) => set((state) => ({ tasks: [...state.tasks, task] })),
  updateTaskPhase: (id, phase) => set((state) => ({
    tasks: state.tasks.map(t => t.id === id ? { ...t, phase } : t)
  }))
}));
