import { create } from 'zustand';

export interface Node {
  id: string;
  provider: 'AWS' | 'GCP';
  status: 'polling' | 'disconnected' | 'executing';
  lastSeen: Date;
}

export interface Task {
  id: string;
  nodeId: string;
  phase: 'planning' | 'executing' | 'completed' | 'failed';
  title: string;
  startedAt: Date;
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
  tasks: Array.from({ length: 24 }).map((_, i) => ({
    id: `task-${1000 + i}`,
    nodeId: i % 3 === 0 ? 'aws-node-01' : i % 3 === 1 ? 'aws-node-02' : 'gcp-node-01',
    phase: i < 5 ? 'completed' : i < 15 ? 'executing' : i < 22 ? 'planning' : 'failed',
    title: `Sub-task ${i + 1}: ${['Refactor Auth', 'Setup DB', 'Fix CSS', 'Write Tests', 'Deploy CI'][i % 5]}`,
    startedAt: new Date(Date.now() - Math.random() * 1000000)
  })),
  
  addNode: (node) => set((state) => ({ nodes: [...state.nodes, node] })),
  updateNodeStatus: (id, status) => set((state) => ({
    nodes: state.nodes.map(n => n.id === id ? { ...n, status, lastSeen: new Date() } : n)
  })),
  
  addTask: (task) => set((state) => ({ tasks: [...state.tasks, task] })),
  updateTaskPhase: (id, phase) => set((state) => ({
    tasks: state.tasks.map(t => t.id === id ? { ...t, phase } : t)
  }))
}));
