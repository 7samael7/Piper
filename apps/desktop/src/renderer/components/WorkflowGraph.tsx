import { Background, Controls, MiniMap, ReactFlow, type Edge, type Node } from "@xyflow/react";
import type { WorkflowDetails } from "@piper/shared-types";
import { useMemo } from "react";
import type { ExecutionState } from "../store/piperStore";

interface WorkflowGraphProps {
  workflow?: WorkflowDetails;
  selectedJobId: string;
  onSelectJob: (jobId: string) => void;
  jobStates: Record<string, ExecutionState>;
}

export function WorkflowGraph({ workflow, selectedJobId, onSelectJob, jobStates }: WorkflowGraphProps) {
  const { nodes, edges } = useMemo(() => toFlowElements(workflow, jobStates), [jobStates, workflow]);

  if (!workflow) {
    return <div className="empty-state">Open a repository and select a workflow.</div>;
  }

  return (
    <ReactFlow
      nodes={nodes.map((node) => ({
        ...node,
        selected: node.id === selectedJobId,
      }))}
      edges={edges}
      fitView
      minZoom={0.35}
      onNodeClick={(_, node) => onSelectJob(node.id)}
      nodesDraggable
    >
      <Background gap={22} color="#d7ded1" />
      <MiniMap pannable zoomable nodeStrokeWidth={3} />
      <Controls />
    </ReactFlow>
  );
}

function toFlowElements(workflow: WorkflowDetails | undefined, jobStates: Record<string, ExecutionState>): { nodes: Node[]; edges: Edge[] } {
  if (!workflow) {
    return { nodes: [], edges: [] };
  }
  const levels = new Map<string, number>();
  const needsByJob = new Map<string, string[]>();
  for (const node of workflow.graph.nodes) {
    needsByJob.set(node.id, []);
  }
  for (const edge of workflow.graph.edges) {
    needsByJob.get(edge.target)?.push(edge.source);
  }

  const resolveLevel = (id: string): number => {
    if (levels.has(id)) {
      return levels.get(id) ?? 0;
    }
    const needs = needsByJob.get(id) ?? [];
    const level = needs.length === 0 ? 0 : Math.max(...needs.map(resolveLevel)) + 1;
    levels.set(id, level);
    return level;
  };

  const byLevel = new Map<number, number>();
  const nodes = workflow.graph.nodes.map((node) => {
    const level = resolveLevel(node.id);
    const row = byLevel.get(level) ?? 0;
    byLevel.set(level, row + 1);
    const state = jobStates[node.id];
    return {
      id: node.id,
      position: { x: level * 280, y: row * 126 },
      data: { label: `${node.label}${state?.status ? ` · ${state.status}` : ""}` },
      className: `flow-node support-node-${node.support}${state?.status ? ` status-${state.status}` : ""}`,
      style: {
        width: 210,
        minHeight: 64,
      },
    } satisfies Node;
  });

  const edges = workflow.graph.edges.map((edge) => ({
    id: edge.id,
    source: edge.source,
    target: edge.target,
    animated: true,
    type: "smoothstep",
  })) satisfies Edge[];

  return { nodes, edges };
}
