import { writable, type Writable } from 'svelte/store';

export const pendingLeadAgent: Writable<string | null> = writable(null);

export function setPendingLeadAgent(agentId: string) {
  pendingLeadAgent.set(agentId);
}

export function clearPendingLeadAgent() {
  pendingLeadAgent.set(null);
}
