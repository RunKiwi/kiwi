import { create } from 'zustand';

export interface Node {
  id: string;
  provider: 'AWS' | 'GCP';
  status: 'polling' | 'disconnected' | 'executing';
  lastSeen: Date;
  cpuUsage: number; // percentage
  ramUsage: number; // percentage
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
  status: 'open' | 'merged' | 'closed';
}

export interface Task {
  id: string;
  phase: 'planning' | 'executing' | 'completed' | 'failed';
  title: string;
  startedAt: Date;
  subAgents: SubAgent[];
  pullRequests: PullRequest[];
}

export interface ProviderConfig {
  id: string;
  name: 'OpenAI' | 'Anthropic' | 'Google' | 'Cohere' | 'Meta';
  isConfigured: boolean;
  availableModels: { id: string; name: string; }[];
}

export interface Integration {
  id: string;
  name: 'GitHub' | 'Jira' | 'Linear' | 'Slack' | 'Discord';
  status: 'connected' | 'disconnected';
  workspace?: string;
}

export interface Repository {
  id: string;
  name: string;
  url: string;
}

interface FleetState {
  nodes: Node[];
  tasks: Task[];
  providers: ProviderConfig[];
  integrations: Integration[];
  repositories: Repository[];
  addNode: (node: Node) => void;
  updateNodeStatus: (id: string, status: Node['status']) => void;
  addTask: (task: Task) => void;
  updateTaskPhase: (id: string, phase: Task['phase']) => void;
}

export const useFleetStore = create<FleetState>((set) => ({
  nodes: [
    { id: 'aws-node-01', provider: 'AWS', status: 'executing', lastSeen: new Date(), cpuUsage: 78, ramUsage: 64 },
    { id: 'aws-node-02', provider: 'AWS', status: 'executing', lastSeen: new Date(), cpuUsage: 45, ramUsage: 82 },
    { id: 'gcp-node-01', provider: 'GCP', status: 'polling', lastSeen: new Date(), cpuUsage: 12, ramUsage: 18 },
    { id: 'gcp-node-02', provider: 'GCP', status: 'disconnected', lastSeen: new Date(Date.now() - 3600000), cpuUsage: 0, ramUsage: 0 },
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

    let prs: { id: string, status: 'open' | 'merged' | 'closed' }[] = [];
    if (i === 1) prs = [{ id: `pr-1a`, status: 'merged' }, { id: `pr-1b`, status: 'merged' }]; // Multiple closed
    else if (i === 3) prs = [{ id: `pr-3a`, status: 'open' }, { id: `pr-3b`, status: 'open' }, { id: `pr-3c`, status: 'open' }]; // Multiple open
    else if (i === 5) prs = [{ id: `pr-5a`, status: 'merged' }, { id: `pr-5b`, status: 'open' }]; // Mixed
    else if (i === 7) prs = [{ id: `pr-7`, status: 'open' }, { id: `pr-8`, status: 'closed' }, { id: `pr-9`, status: 'merged' }]; // All three states

    return {
      id: `task-${1000 + i}`,
      phase,
      title: `Goal: ${['Deploy Microservices', 'Run Security Audit', 'Provision EKS Cluster', 'Migrate Database', 'Run CI/CD Pipeline', 'Backup Vault', 'Scale Up Workers', 'Update Certificates'][i]}`,
      startedAt: new Date(Date.now() - Math.random() * 1000000),
      subAgents,
      pullRequests: prs
    };
  }),
  providers: [
    { 
      id: 'p-1', 
      name: 'Anthropic', 
      isConfigured: true,
      availableModels: [
        { id: 'claude-3-5-sonnet', name: 'Claude 3.5 Sonnet' },
        { id: 'claude-3-haiku', name: 'Claude 3 Haiku' }
      ]
    },
    { 
      id: 'p-2', 
      name: 'OpenAI', 
      isConfigured: true,
      availableModels: [
        { id: 'gpt-4o', name: 'GPT-4o' },
        { id: 'gpt-4o-mini', name: 'GPT-4o-mini' }
      ]
    },
    { 
      id: 'p-3', 
      name: 'Google', 
      isConfigured: false,
      availableModels: [
        { id: 'gemini-1-5-pro', name: 'Gemini 1.5 Pro' },
        { id: 'gemini-1-5-flash', name: 'Gemini 1.5 Flash' }
      ]
    },
  ],
  integrations: [
    { id: 'i-1', name: 'GitHub', status: 'connected', workspace: 'RunKiwi' },
    { id: 'i-2', name: 'Slack', status: 'connected', workspace: 'KiwiTeam' },
    { id: 'i-3', name: 'Jira', status: 'disconnected' },
    { id: 'i-4', name: 'Linear', status: 'connected', workspace: 'KiwiHQ' },
    { id: 'i-5', name: 'Discord', status: 'disconnected' },
  ],
  repositories: [
    { id: 'r-1', name: 'RunKiwi/kiwi', url: 'https://github.com/RunKiwi/kiwi' },
    { id: 'r-2', name: 'RunKiwi/steelwing', url: 'https://github.com/RunKiwi/steelwing' },
    { id: 'r-3', name: 'RunKiwi/docs', url: 'https://github.com/RunKiwi/docs' },
  ],
  
  addNode: (node) => set((state) => ({ nodes: [...state.nodes, node] })),
  updateNodeStatus: (id, status) => set((state) => ({
    nodes: state.nodes.map(n => n.id === id ? { ...n, status, lastSeen: new Date() } : n)
  })),
  
  addTask: (task) => set((state) => ({ tasks: [...state.tasks, task] })),
  updateTaskPhase: (id, phase) => set((state) => ({
    tasks: state.tasks.map(t => t.id === id ? { ...t, phase } : t)
  }))
}));
