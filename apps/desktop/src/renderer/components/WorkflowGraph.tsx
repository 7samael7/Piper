import { Background, Controls, MiniMap, ReactFlow, type Edge, type Node, type ReactFlowInstance } from "@xyflow/react";
import type { GraphNode, WorkflowDetails } from "@piper/shared-types";
import { useCallback, useEffect, useMemo, useRef } from "react";
import type { ExecutionState } from "../store/piperStore";

interface WorkflowGraphProps {
  workflow?: WorkflowDetails;
  selectedJobId: string;
  onSelectJob: (jobId: string) => void;
  jobStates: Record<string, ExecutionState>;
}

export function WorkflowGraph({ workflow, selectedJobId, onSelectJob, jobStates }: WorkflowGraphProps) {
  const { nodes, edges } = useMemo(() => toFlowElements(workflow, jobStates), [jobStates, workflow]);
  const selectedNodes: Node[] = nodes.map((node) => ({
    ...node,
    selected: node.id === selectedJobId,
  }));
  const containerRef = useRef<HTMLDivElement | null>(null);
  const flowRef = useRef<ReactFlowInstance | null>(null);
  const fitFrameRef = useRef(0);
  const fitGraph = useCallback(() => {
    window.cancelAnimationFrame(fitFrameRef.current);
    fitFrameRef.current = window.requestAnimationFrame(() => {
      const container = containerRef.current;
      if (!container || container.clientWidth === 0 || container.clientHeight === 0) {
        return;
      }
      void flowRef.current?.fitView({ padding: 0.22, maxZoom: 0.85 });
    });
  }, []);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) {
      return;
    }
    const observer = new ResizeObserver(fitGraph);
    observer.observe(container);
    fitGraph();
    return () => {
      observer.disconnect();
      window.cancelAnimationFrame(fitFrameRef.current);
    };
  }, [fitGraph]);

  useEffect(() => {
    fitGraph();
  }, [edges, fitGraph, nodes]);

  if (!workflow) {
    return <div className="empty-state">Open a repository and select a workflow.</div>;
  }

  return (
    <div ref={containerRef} className="workflow-graph-canvas">
      <ReactFlow
        nodes={selectedNodes}
        edges={edges}
        fitView
        fitViewOptions={{ padding: 0.22, maxZoom: 0.85 }}
        minZoom={0.1}
        maxZoom={1}
        onInit={(instance) => {
          flowRef.current = instance;
          fitGraph();
        }}
        onNodeClick={(_, node) => onSelectJob(node.id)}
        nodesDraggable
      >
        <Background gap={18} color="#d7ded1" />
        <MiniMap pannable zoomable nodeStrokeWidth={2} style={{ width: 120, height: 76 }} />
        <Controls />
      </ReactFlow>
    </div>
  );
}

const NODE_WIDTH = 150;
const NODE_MIN_HEIGHT = 44;
const COLUMN_STEP = 210;
const ROW_STEP = 78;

function toFlowElements(workflow: WorkflowDetails | undefined, jobStates: Record<string, ExecutionState>): { nodes: Node[]; edges: Edge[] } {
  if (!workflow) {
    return { nodes: [], edges: [] };
  }
  const needsByJob = new Map<string, string[]>();
  for (const node of workflow.graph.nodes) {
    needsByJob.set(node.id, []);
  }
  for (const edge of workflow.graph.edges) {
    needsByJob.get(edge.target)?.push(edge.source);
  }

  // Resolve each node's dependency depth (its column). Memoized, with a guard so a
  // malformed cyclic graph degrades to a root instead of recursing forever.
  const levels = new Map<string, number>();
  const visiting = new Set<string>();
  const resolveLevel = (id: string): number => {
    const cached = levels.get(id);
    if (cached !== undefined) {
      return cached;
    }
    if (visiting.has(id)) {
      return 0;
    }
    visiting.add(id);
    const needs = needsByJob.get(id) ?? [];
    const level = needs.length === 0 ? 0 : Math.max(...needs.map(resolveLevel)) + 1;
    visiting.delete(id);
    levels.set(id, level);
    return level;
  };

  // Group nodes by depth, preserving input order within each level.
  const nodesByLevel = new Map<number, GraphNode[]>();
  for (const node of workflow.graph.nodes) {
    const level = resolveLevel(node.id);
    const bucket = nodesByLevel.get(level);
    if (bucket) {
      bucket.push(node);
    } else {
      nodesByLevel.set(level, [node]);
    }
  }

  // Wide levels (e.g. many parallel test jobs with no dependencies) would otherwise
  // stack into one endlessly tall column. Cap each column's height and wrap the
  // overflow into extra sub-columns so the graph keeps a sane aspect ratio.
  const total = workflow.graph.nodes.length;
  const maxRowsPerColumn = Math.max(4, Math.ceil(Math.sqrt(total) * 1.4));

  const sortedLevels = [...nodesByLevel.keys()].sort((a, b) => a - b);
  const columnOf = new Map<string, number>();
  const rowOf = new Map<string, number>();
  let columnCursor = 0;
  let maxHeight = 0;
  for (const level of sortedLevels) {
    const bucket = nodesByLevel.get(level) ?? [];
    bucket.forEach((node, index) => {
      // Fill each sub-column top-to-bottom before starting the next.
      columnOf.set(node.id, columnCursor + Math.floor(index / maxRowsPerColumn));
      rowOf.set(node.id, index % maxRowsPerColumn);
    });
    const rowsUsed = Math.min(bucket.length, maxRowsPerColumn);
    maxHeight = Math.max(maxHeight, rowsUsed * ROW_STEP);
    columnCursor += Math.max(1, Math.ceil(bucket.length / maxRowsPerColumn));
  }

  const nodes = workflow.graph.nodes.map((node) => {
    const level = resolveLevel(node.id);
    const rowsUsed = Math.min(nodesByLevel.get(level)?.length ?? 1, maxRowsPerColumn);
    // Center each level's column block vertically so the whole graph stays balanced.
    const yOffset = (maxHeight - rowsUsed * ROW_STEP) / 2;
    const state = jobStates[node.id];
    return {
      id: node.id,
      position: {
        x: (columnOf.get(node.id) ?? 0) * COLUMN_STEP,
        y: yOffset + (rowOf.get(node.id) ?? 0) * ROW_STEP,
      },
      data: { label: `${node.label}${state?.status ? ` · ${state.status}` : ""}` },
      className: `flow-node support-node-${node.support}${state?.status ? ` status-${state.status}` : ""}`,
      style: {
        width: NODE_WIDTH,
        minHeight: NODE_MIN_HEIGHT,
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
