import { useEffect, useMemo, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import type { RunEvent, RunStatus, WorkflowDetails } from "@piper/shared-types";

interface LogTerminalProps {
  events: RunEvent[];
  workflow?: WorkflowDetails;
  selectedJobId: string;
  onSelectJob: (jobId: string) => void;
}

interface LogTab {
  id: string;
  name: string;
  stage: string;
  status?: RunStatus;
  eventCount: number;
}

const runTabId = "__run__";

export function LogTerminal({ events, workflow, selectedJobId, onSelectJob }: LogTerminalProps) {
  const tabs = useMemo(() => buildLogTabs(workflow, events), [events, workflow]);
  const [activeTabId, setActiveTabId] = useState(selectedJobId || runTabId);

  useEffect(() => {
    setActiveTabId(selectedJobId || runTabId);
  }, [selectedJobId]);

  useEffect(() => {
    if (!tabs.some((tab) => tab.id === activeTabId)) {
      setActiveTabId(selectedJobId && tabs.some((tab) => tab.id === selectedJobId) ? selectedJobId : runTabId);
    }
  }, [activeTabId, selectedJobId, tabs]);

  const activeTab = tabs.find((tab) => tab.id === activeTabId) ?? tabs[0];
  const eventsByTab = useMemo(() => {
    const groupedEvents = new Map(tabs.map((tab) => [tab.id, [] as RunEvent[]]));
    for (const event of events) {
      groupedEvents.get(event.jobId ?? runTabId)?.push(event);
    }
    return groupedEvents;
  }, [events, tabs]);

  return (
    <>
      <div className="log-tabs" role="tablist" aria-label="Job log streams">
        {tabs.map((tab) => (
          <button
            key={tab.id}
            type="button"
            role="tab"
            aria-selected={tab.id === activeTab.id}
            className={tab.id === activeTab.id ? "log-tab active" : "log-tab"}
            onClick={() => {
              setActiveTabId(tab.id);
              onSelectJob(tab.id === runTabId ? "" : tab.id);
            }}
            title={tab.id === runTabId ? "Run-level output" : `${tab.stage}: ${tab.name}`}
          >
            <span className={`log-tab-status${tab.status ? ` status-${tab.status}` : ""}`} aria-hidden="true" />
            <span className="log-tab-label">
              <small>{tab.stage}</small>
              <strong>{tab.name}</strong>
            </span>
            {tab.eventCount > 0 ? <span className="log-tab-count">{tab.eventCount}</span> : null}
          </button>
        ))}
      </div>
      <div className="terminal-stack">
        {tabs.map((tab) => (
          <TerminalStream
            key={tab.id}
            active={tab.id === activeTab.id}
            events={eventsByTab.get(tab.id) ?? []}
            emptyMessage={tab.id === runTabId ? "No run-level output yet." : `No output from ${tab.name} yet.`}
          />
        ))}
      </div>
    </>
  );
}

function TerminalStream({ active, events, emptyMessage }: { active: boolean; events: RunEvent[]; emptyMessage: string }) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const renderedEventCountRef = useRef(0);

  useEffect(() => {
    if (!containerRef.current || terminalRef.current) {
      return;
    }
    const terminal = new Terminal({
      convertEol: true,
      fontFamily: "JetBrains Mono, SFMono-Regular, Menlo, Consolas, monospace",
      fontSize: 12,
      theme: {
        background: "#111714",
        foreground: "#dce7dd",
        cursor: "#f0c05a",
        selectionBackground: "#365348",
      },
    });
    const fit = new FitAddon();
    terminal.loadAddon(fit);
    terminal.open(containerRef.current);
    terminalRef.current = terminal;
    fitRef.current = fit;

    return () => {
      terminal.dispose();
      terminalRef.current = null;
    };
  }, []);

  useEffect(() => {
    const container = containerRef.current;
    if (!active || !container) {
      return;
    }
    let animationFrame = 0;
    const scheduleFit = () => {
      window.cancelAnimationFrame(animationFrame);
      animationFrame = window.requestAnimationFrame(() => {
        if (container.clientWidth === 0 || container.clientHeight === 0) {
          return;
        }
        fitRef.current?.fit();
        terminalRef.current?.scrollToBottom();
      });
    };
    const resizeObserver = new ResizeObserver(scheduleFit);
    resizeObserver.observe(container);
    scheduleFit();
    return () => {
      window.cancelAnimationFrame(animationFrame);
      resizeObserver.disconnect();
    };
  }, [active]);

  useEffect(() => {
    const terminal = terminalRef.current;
    if (!terminal) {
      return;
    }
    if (events.length === 0) {
      terminal.reset();
      renderedEventCountRef.current = 0;
      terminal.writeln(`\x1b[90m${emptyMessage}\x1b[0m`);
      return;
    }

    if (events.length < renderedEventCountRef.current || renderedEventCountRef.current === 0) {
      terminal.reset();
      renderedEventCountRef.current = 0;
    }
    for (const event of events.slice(renderedEventCountRef.current)) {
      terminal.writeln(formatEvent(event));
    }
    renderedEventCountRef.current = events.length;
    terminal.scrollToBottom();
    if (active) {
      fitRef.current?.fit();
    }
  }, [active, emptyMessage, events]);

  return <div ref={containerRef} className={active ? "terminal-host active" : "terminal-host"} aria-hidden={!active} />;
}

function buildLogTabs(workflow: WorkflowDetails | undefined, events: RunEvent[]): LogTab[] {
  const statusByJob = new Map<string, RunStatus>();
  const eventCountByJob = new Map<string, number>();
  let runStatus: RunStatus | undefined;
  let runEventCount = 0;

  for (const event of events) {
    if (!event.jobId) {
      runEventCount += 1;
      if (event.status) {
        runStatus = event.status;
      }
      continue;
    }
    eventCountByJob.set(event.jobId, (eventCountByJob.get(event.jobId) ?? 0) + 1);
    if (event.status && !event.stepId) {
      statusByJob.set(event.jobId, event.status);
    }
  }

  const tabs: LogTab[] = [{
    id: runTabId,
    name: "Overview",
    stage: "Run",
    status: runStatus,
    eventCount: runEventCount,
  }];
  const knownJobIds = new Set<string>();

  if (workflow?.executionPlan?.jobs.length) {
    const logicalJobs = new Map(workflow.jobs.map((job) => [job.id, job]));
    for (const job of workflow.executionPlan.jobs) {
      const logicalJob = logicalJobs.get(job.logicalJobId);
      knownJobIds.add(job.id);
      tabs.push({
        id: job.id,
        name: job.name,
        stage: logicalJob?.stage || "Job",
        status: statusByJob.get(job.id),
        eventCount: eventCountByJob.get(job.id) ?? 0,
      });
    }
  } else {
    for (const job of workflow?.jobs ?? []) {
      knownJobIds.add(job.id);
      tabs.push({
        id: job.id,
        name: job.name,
        stage: job.stage || "Job",
        status: statusByJob.get(job.id),
        eventCount: eventCountByJob.get(job.id) ?? 0,
      });
    }
  }

  for (const event of events) {
    if (!event.jobId || knownJobIds.has(event.jobId)) {
      continue;
    }
    knownJobIds.add(event.jobId);
    tabs.push({
      id: event.jobId,
      name: event.jobId,
      stage: "Job",
      status: statusByJob.get(event.jobId),
      eventCount: eventCountByJob.get(event.jobId) ?? 0,
    });
  }

  return tabs;
}

function formatEvent(event: RunEvent) {
  const time = new Date(event.time).toLocaleTimeString();
  const scope = [event.jobId, event.stepId].filter(Boolean).join("/");
  const prefix = `[${time}] ${event.type}${scope ? ` ${scope}` : ""}`;
  const color = event.stream === "stderr" || event.status === "failed" ? "\x1b[31m" : event.stream === "stdout" ? "\x1b[37m" : "\x1b[36m";
  return `${color}${prefix}\x1b[0m ${event.message}`;
}
