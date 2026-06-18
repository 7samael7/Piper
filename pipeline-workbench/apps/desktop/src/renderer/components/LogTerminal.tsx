import { useEffect, useRef } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import type { RunEvent } from "@pipeline-workbench/shared-types";

interface LogTerminalProps {
  events: RunEvent[];
}

export function LogTerminal({ events }: LogTerminalProps) {
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
    fit.fit();
    terminalRef.current = terminal;
    fitRef.current = fit;

    const onResize = () => fit.fit();
    window.addEventListener("resize", onResize);
    return () => {
      window.removeEventListener("resize", onResize);
      terminal.dispose();
      terminalRef.current = null;
    };
  }, []);

  useEffect(() => {
    const terminal = terminalRef.current;
    if (!terminal) {
      return;
    }
    if (events.length === 0) {
      terminal.reset();
      renderedEventCountRef.current = 0;
      terminal.writeln("\x1b[90mNo active log stream.\x1b[0m");
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
    fitRef.current?.fit();
  }, [events]);

  return <div ref={containerRef} className="terminal-host" />;
}

function formatEvent(event: RunEvent) {
  const time = new Date(event.time).toLocaleTimeString();
  const scope = [event.jobId, event.stepId].filter(Boolean).join("/");
  const prefix = `[${time}] ${event.type}${scope ? ` ${scope}` : ""}`;
  const color = event.stream === "stderr" || event.status === "failed" ? "\x1b[31m" : event.stream === "stdout" ? "\x1b[37m" : "\x1b[36m";
  return `${color}${prefix}\x1b[0m ${event.message}`;
}
