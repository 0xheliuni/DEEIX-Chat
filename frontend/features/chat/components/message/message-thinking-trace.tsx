"use client";

import * as React from "react";

import { Brain } from "lucide-react";

import { ChevronDown } from "@/components/animate-ui/icons/chevron-down";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import { Spinner } from "@/components/ui/spinner";
import { Marker, MarkerContent } from "@/components/ui/marker";
import type { ChatTraceBlock, ChatTraceEvent } from "@/features/chat/types/messages";
import {
  useProcessTraceLabels,
  type ProcessTraceLabels,
} from "@/features/chat/hooks/use-process-trace-labels";
import {
  AgentToolStepRow,
  buildToolGroupSteps,
  hasActiveToolTraceCalls,
  summarizeToolChainSteps,
  type ToolChainStep,
} from "@/features/chat/components/message/message-tool-trace";
import { StreamdownRender } from "@/shared/components/markdown/streamdown-render";
import { cn } from "@/lib/utils";
import { TRACE_ROOT_CLASS } from "@/features/chat/components/shared/message-process-trace-shared";
import type { TraceDisplayEvent } from "@/features/chat/model/message-process-trace";

function traceEventToBlock(event: ChatTraceEvent): ChatTraceBlock {
  return {
    title: event.title,
    summary: event.summary,
    contentMarkdown: event.contentMarkdown,
    status: event.status,
    stage: event.stage,
    roundID: event.roundID,
    parentEventID: event.parentEventID,
    updatedAt: event.updatedAt,
    payloadJson: event.payloadJson,
  };
}

function isToolTraceEvent(event: ChatTraceEvent): boolean {
  if (event.stage === "think" || event.phase === "upstream_think" || event.eventType === "think") {
    return false;
  }
  return event.stage === "tool" || event.phase === "tools" || event.eventType === "tool";
}

function isThinkTraceEvent(event: ChatTraceEvent): boolean {
  return event.stage === "think" || event.phase === "upstream_think" || event.eventType === "think";
}

function buildTraceDisplayEvents(events: ChatTraceEvent[]): TraceDisplayEvent[] {
  return events
    .filter((event) => isToolTraceEvent(event) || isThinkTraceEvent(event))
    .sort((left, right) => left.seq - right.seq)
    .map((event) => {
      if (isThinkTraceEvent(event)) {
        return { event, kind: "think" };
      }
      return { event, kind: "tool" };
    });
}

function traceBlockDisplayText(block: Pick<ChatTraceBlock, "contentMarkdown" | "summary">): string {
  return block.contentMarkdown?.trim() || block.summary?.trim() || "";
}

type OrderedThinkBlock = ChatTraceBlock & {
  seq: number;
};

function mergeThinkTraceBlock(events: TraceDisplayEvent[], activeThinkBlock?: ChatTraceBlock): ChatTraceBlock | undefined {
  const blocks: OrderedThinkBlock[] = events
    .filter((item) => item.kind === "think")
    .map((item) => ({ ...traceEventToBlock(item.event), seq: item.event.seq }));

  if (activeThinkBlock) {
    const activeText = traceBlockDisplayText(activeThinkBlock);
    const activeIndex = blocks.findIndex((block) => {
      const sameRound = Boolean(activeThinkBlock.roundID && block.roundID === activeThinkBlock.roundID);
      const sameParent = Boolean(activeThinkBlock.parentEventID && block.parentEventID === activeThinkBlock.parentEventID);
      const sameText = Boolean(activeText && traceBlockDisplayText(block) === activeText);
      return sameRound || sameParent || sameText;
    });
    if (activeIndex >= 0) {
      blocks[activeIndex] = { ...activeThinkBlock, seq: blocks[activeIndex].seq };
    } else {
      blocks.push({ ...activeThinkBlock, seq: Number.MAX_SAFE_INTEGER });
    }
  }

  if (blocks.length === 0) {
    return undefined;
  }

  const ordered = [...blocks].sort((left, right) => left.seq - right.seq);
  const parts: string[] = [];
  for (const block of ordered) {
    const text = traceBlockDisplayText(block);
    if (text && !parts.includes(text)) {
      parts.push(text);
    }
  }
  if (parts.length === 0) {
    return undefined;
  }

  const latest = ordered[ordered.length - 1];
  return {
    ...latest,
    stage: "think",
    status: ordered.some((block) => block.status === "streaming") ? "streaming" : latest.status || "completed",
    contentMarkdown: parts.join("\n\n"),
    contentSegments: parts,
  };
}

type TraceRoundGroup = {
  key: string;
  seq: number;
  thinkEvents: TraceDisplayEvent[];
  toolEvents: TraceDisplayEvent[];
  thinkBlock?: ChatTraceBlock;
  toolBlock?: ChatTraceBlock;
};

function traceRoundGroupKey(event: Pick<ChatTraceEvent, "roundID" | "eventID" | "seq">, kind: "think" | "tool"): string {
  const roundID = event.roundID?.trim() || "";
  if (roundID) {
    return `${kind}:${roundID}`;
  }
  return `${kind}:${event.eventID || event.seq}`;
}

/**
 * Group think and tool events into agent-loop rounds so each thinking step can be
 * rendered right above the tool calls it produced. Think events key their own
 * round; tool events join the preceding think round via roundID / parentEventID
 * and keep a standalone group when the round ran without thinking.
 */
function groupTraceDisplayEvents(
  displayEvents: TraceDisplayEvent[],
  activeThinkBlock?: ChatTraceBlock,
  activeToolBlock?: ChatTraceBlock,
): TraceRoundGroup[] {
  const groups = new Map<string, TraceRoundGroup>();
  const thinkEventIDToKey = new Map<string, string>();
  const thinkRoundIDToKey = new Map<string, string>();

  const ensureGroup = (key: string, seq: number): TraceRoundGroup => {
    let group = groups.get(key);
    if (!group) {
      group = { key, seq, thinkEvents: [], toolEvents: [] };
      groups.set(key, group);
    } else if (seq < group.seq) {
      group.seq = seq;
    }
    return group;
  };

  for (const item of displayEvents) {
    if (item.kind !== "think") {
      continue;
    }
    const key = traceRoundGroupKey(item.event, "think");
    ensureGroup(key, item.event.seq).thinkEvents.push(item);
    if (item.event.roundID?.trim()) {
      thinkRoundIDToKey.set(item.event.roundID.trim(), key);
    }
    if (item.event.eventID) {
      thinkEventIDToKey.set(item.event.eventID, key);
    }
  }

  for (const item of displayEvents) {
    if (item.kind !== "tool") {
      continue;
    }
    const roundID = item.event.roundID?.trim() || "";
    const parentID = item.event.parentEventID?.trim() || "";
    const key =
      (roundID && thinkRoundIDToKey.get(roundID)) ||
      (parentID && thinkEventIDToKey.get(parentID)) ||
      traceRoundGroupKey(item.event, "tool");
    ensureGroup(key, item.event.seq).toolEvents.push(item);
  }

  // Live streaming blocks join their own round; unmatched blocks are appended last.
  const attachActiveBlock = (block: ChatTraceBlock | undefined, kind: "think" | "tool") => {
    if (!block) {
      return;
    }
    const roundID = block.roundID?.trim() || "";
    const parentID = block.parentEventID?.trim() || "";
    const matchedKey = (roundID && thinkRoundIDToKey.get(roundID)) || (parentID && thinkEventIDToKey.get(parentID));
    if (matchedKey) {
      const matched = groups.get(matchedKey);
      if (matched) {
        if (kind === "think") {
          matched.thinkBlock = block;
        } else {
          matched.toolBlock = block;
        }
        return;
      }
    }
    const toolOnlyKey = `tool:${roundID}`;
    if (roundID && groups.has(toolOnlyKey)) {
      const toolOnly = groups.get(toolOnlyKey);
      if (toolOnly) {
        if (kind === "think") {
          toolOnly.thinkBlock = block;
        } else {
          toolOnly.toolBlock = block;
        }
        return;
      }
    }
    const key = `active:${kind}:${roundID || parentID || "latest"}`;
    const group = ensureGroup(key, Number.MAX_SAFE_INTEGER);
    if (kind === "think") {
      group.thinkBlock = block;
    } else {
      group.toolBlock = block;
    }
  };
  attachActiveBlock(activeThinkBlock, "think");
  attachActiveBlock(activeToolBlock, "tool");

  return [...groups.values()].sort((left, right) => left.seq - right.seq);
}

function thinkEventDurationMS(thinkEvents: TraceDisplayEvent[]): number | undefined {
  let total = 0;
  for (const item of thinkEvents) {
    const { startedAt, endedAt, updatedAt } = item.event;
    if (!startedAt) {
      continue;
    }
    const startMS = new Date(startedAt).getTime();
    if (!Number.isFinite(startMS)) {
      continue;
    }
    // 部分历史事件只有 startedAt（未走到 complete 落盘），用快照更新时间兜底。
    const rawEnd = endedAt?.trim() ? endedAt : updatedAt;
    if (!rawEnd) {
      continue;
    }
    const endMS = new Date(rawEnd).getTime();
    if (!Number.isFinite(endMS) || endMS <= startMS) {
      continue;
    }
    total += endMS - startMS;
  }
  return total > 0 ? total : undefined;
}

function thinkBlockDurationMS(block: ChatTraceBlock): number | undefined {
  const { startedAt, updatedAt } = block;
  if (!startedAt || !updatedAt) {
    return undefined;
  }
  const startMS = new Date(startedAt).getTime();
  const endMS = new Date(updatedAt).getTime();
  if (!Number.isFinite(startMS) || !Number.isFinite(endMS) || endMS <= startMS) {
    return undefined;
  }
  return endMS - startMS;
}

function formatThinkDuration(durationMS: number | undefined): string | undefined {
  if (!durationMS || durationMS <= 0) {
    return undefined;
  }
  if (durationMS < 10000) {
    return `${(durationMS / 1000).toFixed(1)}s`;
  }
  return `${Math.round(durationMS / 1000)}s`;
}

function formatRunDuration(durationMS: number | undefined): string | undefined {
  if (!durationMS || durationMS <= 0) {
    return undefined;
  }
  const wholeSeconds = Math.max(1, Math.round(durationMS / 1000));
  if (wholeSeconds < 60) {
    return `${wholeSeconds}s`;
  }
  const minutes = Math.floor(wholeSeconds / 60);
  const seconds = wholeSeconds % 60;
  return `${minutes}m ${seconds}s`;
}

function isGroupToolStreaming(group: TraceRoundGroup, messageStreaming: boolean): boolean {
  if (!messageStreaming) {
    return false;
  }
  if (group.toolBlock?.status === "streaming" || hasActiveToolTraceCalls(group.toolBlock?.payloadJson)) {
    return true;
  }
  return group.toolEvents.some(
    (item) => item.event.status === "streaming" || hasActiveToolTraceCalls(item.event.payloadJson),
  );
}

type TraceTimelineItem =
  | { kind: "think"; key: string; block: ChatTraceBlock; streaming: boolean; durationMS?: number }
  | { kind: "tool"; key: string; step: ToolChainStep };

function TraceThinkRow({
  block,
  streaming,
  durationMS,
  autoCollapseReady,
  labels,
}: {
  block: ChatTraceBlock;
  streaming: boolean;
  durationMS?: number;
  autoCollapseReady?: boolean;
  labels: ProcessTraceLabels;
}) {
  const [open, setOpen] = React.useState(streaming);
  const wasStreamingRef = React.useRef(streaming);

  React.useEffect(() => {
    if (streaming) {
      setOpen(true);
      wasStreamingRef.current = true;
      return;
    }
    if (wasStreamingRef.current && autoCollapseReady) {
      setOpen(false);
    }
    if (autoCollapseReady) {
      wasStreamingRef.current = false;
    }
  }, [autoCollapseReady, streaming]);

  const durationText = formatThinkDuration(durationMS);

  return (
    <li className="group/trace-think-row">
      <div className="grid grid-cols-[0.875rem_minmax(0,1fr)] items-start gap-x-5 text-[12px] leading-5 max-sm:gap-x-2">
        <div className="relative flex justify-center">
          <span
            className={cn(
              "relative z-10 mt-[0.3rem] inline-flex size-3.5 items-center justify-center rounded-full",
              "border border-border/55 bg-background text-muted-foreground/72",
              "transition-colors group-hover/trace-think-row:text-muted-foreground",
            )}
          >
            {streaming ? <Spinner className="size-2.5" /> : <Brain className="size-2.5" />}
          </span>
        </div>
        <button
          type="button"
          className="flex min-w-0 items-center gap-1 pb-2 text-left"
          onClick={() => setOpen((value) => !value)}
        >
          <span className="shrink-0 font-medium text-muted-foreground/76 transition-colors group-hover/trace-think-row:text-foreground/88">
            {streaming ? labels.think.rowActive : labels.think.rowDone}
          </span>
          {durationText ? (
            <span className="shrink-0 text-muted-foreground/58">{labels.think.duration(durationText)}</span>
          ) : null}
          <ChevronDown
            className={cn(
              "size-3 shrink-0 text-muted-foreground transition-transform duration-200",
              !open && "-rotate-90",
            )}
          />
        </button>
      </div>
      {open ? (
        <div className="pb-2 pl-[calc(0.875rem+1.25rem)] max-sm:pl-[calc(0.875rem+0.5rem)]">
          <StreamdownRender content={block.contentMarkdown} streaming={streaming} variant="thinking" />
        </div>
      ) : null}
    </li>
  );
}

function AgentTraceTimeline({
  items,
  labels,
  autoCollapseReady,
}: {
  items: TraceTimelineItem[];
  labels: ProcessTraceLabels;
  autoCollapseReady?: boolean;
}) {
  return (
    <div className="relative">
      <span aria-hidden className="absolute bottom-2 left-[6px] top-2 w-px bg-border/42" />
      <ol className="space-y-0.5">
        {items.map((item) =>
          item.kind === "think" ? (
            <TraceThinkRow
              key={item.key}
              block={item.block}
              streaming={item.streaming}
              durationMS={item.durationMS}
              autoCollapseReady={autoCollapseReady}
              labels={labels}
            />
          ) : (
            <AgentToolStepRow key={item.key} step={item.step} labels={labels} />
          ),
        )}
      </ol>
    </div>
  );
}

export function MessageTraceEventBlocks({
  events: traceEvents,
  activeToolBlock,
  activeThinkBlock,
  messageStreaming,
  autoCollapseReady,
  runDurationMS,
}: {
  events: ChatTraceEvent[];
  activeToolBlock?: ChatTraceBlock;
  activeThinkBlock?: ChatTraceBlock;
  messageStreaming?: boolean;
  autoCollapseReady?: boolean;
  runDurationMS?: number;
}) {
  const labels = useProcessTraceLabels();
  const displayEvents = React.useMemo(() => buildTraceDisplayEvents(traceEvents), [traceEvents]);
  const groups = React.useMemo(
    () => groupTraceDisplayEvents(displayEvents, activeThinkBlock, activeToolBlock),
    [activeThinkBlock, activeToolBlock, displayEvents],
  );
  const groupToolSteps = React.useMemo(
    () => groups.map((group) => buildToolGroupSteps(group.toolEvents, group.toolBlock, labels)),
    [groups, labels],
  );
  const items = React.useMemo<TraceTimelineItem[]>(() => {
    const list: TraceTimelineItem[] = [];
    // 聚合工具块会包含全部轮次的调用，跨组按调用 ID 去重，避免每个轮次重复出现。
    const seenToolKeys = new Set<string>();
    groups.forEach((group, index) => {
      const thinkBlock = mergeThinkTraceBlock(group.thinkEvents, group.thinkBlock);
      if (thinkBlock) {
        list.push({
          kind: "think",
          key: `${group.key}:think`,
          block: thinkBlock,
          streaming: Boolean(messageStreaming && thinkBlock.status === "streaming"),
          durationMS: thinkEventDurationMS(group.thinkEvents) ?? thinkBlockDurationMS(thinkBlock),
        });
      }
      groupToolSteps[index].forEach((step, stepIndex) => {
        const toolKey = step.toolCallID?.trim()
          ? `id:${step.toolCallID.trim()}`
          : `fb:${step.label}:${step.toolInput?.trim() || ""}`;
        if (toolKey && seenToolKeys.has(toolKey)) {
          return;
        }
        if (toolKey) {
          seenToolKeys.add(toolKey);
        }
        list.push({ kind: "tool", key: `${group.key}:${step.key}:${stepIndex}`, step });
      });
    });
    return list;
  }, [groupToolSteps, groups, messageStreaming]);

  const [accordionValue, setAccordionValue] = React.useState(() => (messageStreaming ? "message-trace-timeline" : ""));
  const wasStreamingRef = React.useRef(Boolean(messageStreaming));

  React.useEffect(() => {
    if (messageStreaming) {
      setAccordionValue("message-trace-timeline");
      wasStreamingRef.current = true;
      return;
    }
    if (wasStreamingRef.current && autoCollapseReady) {
      setAccordionValue("");
    }
    if (autoCollapseReady) {
      wasStreamingRef.current = false;
    }
  }, [autoCollapseReady, messageStreaming]);

  if (items.length === 0) {
    return null;
  }

  const allToolSteps = groupToolSteps.flat();
  const toolSummary = summarizeToolChainSteps(allToolSteps);
  const thinkRounds = items.filter((item) => item.kind === "think").length;
  const thinkStreaming = items.some((item) => item.kind === "think" && item.streaming);
  const toolsStreaming = groups.some((group) => isGroupToolStreaming(group, Boolean(messageStreaming)));
  const durationText = formatRunDuration(runDurationMS);

  const kindChips = toolSummary.kinds.map((kind) => `${kind.label} (${kind.count})`).join(labels.run.listSeparator);
  const subtitleParts: string[] = [];
  if (toolSummary.total > 0) {
    subtitleParts.push(labels.run.toolCallsSummary(toolSummary.total, kindChips));
  }
  if (thinkRounds > 0) {
    subtitleParts.push(labels.run.thinkRounds(thinkRounds));
  }
  const subtitle = subtitleParts.join(labels.run.labelSeparator);

  const resolvedTitle = messageStreaming
    ? thinkStreaming
      ? labels.think.titleActive
      : toolsStreaming
        ? labels.tool.chain.titleActive
        : labels.think.titleActive
    : labels.run.summarySteps(groups.length);
  const title = durationText ? `${resolvedTitle}${labels.run.durationSuffix(durationText)}` : resolvedTitle;
  const open = accordionValue === "message-trace-timeline";

  return (
    <div className={TRACE_ROOT_CLASS}>
      <Accordion
        type="single"
        collapsible
        value={accordionValue}
        onValueChange={(value) => setAccordionValue(value || "")}
        className="w-full"
      >
        <AccordionItem value="message-trace-timeline" className="border-b-0">
          <AccordionTrigger
            iconPosition="none"
            className="group items-start justify-between gap-1.5 py-0 text-left no-underline hover:no-underline"
          >
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-1.5">
                <Marker
                  render={<span />}
                  className={cn(
                    "inline-flex min-h-0 w-auto text-[13px] font-medium transition-colors",
                    !messageStreaming && "text-muted-foreground group-hover:text-foreground",
                  )}
                >
                  <MarkerContent className={cn("min-w-0", messageStreaming && "shimmer")}>{title}</MarkerContent>
                </Marker>
              </div>
              {subtitle ? (
                <div className="mt-0.5 truncate text-[11px] font-normal leading-4 text-muted-foreground/62">{subtitle}</div>
              ) : null}
            </div>
            <ChevronDown
              className={cn(
                "mt-0.5 size-3.5 shrink-0 text-muted-foreground transition-transform duration-200 group-hover:text-foreground",
                open && "rotate-180",
              )}
            />
          </AccordionTrigger>
          <AccordionContent className="px-0 pb-0 pt-1.5 duration-[350ms] ease-in-out">
            <AgentTraceTimeline items={items} labels={labels} autoCollapseReady={autoCollapseReady} />
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    </div>
  );
}

export function MessageUpstreamThink({
  block,
  streaming,
  autoCollapseReady,
  title,
  subtitle,
}: {
  block?: ChatTraceBlock;
  streaming?: boolean;
  autoCollapseReady?: boolean;
  title?: string;
  subtitle?: string;
}) {
  const labels = useProcessTraceLabels();
  const [accordionValue, setAccordionValue] = React.useState(() => (streaming ? "upstream-think" : ""));
  const wasStreamingRef = React.useRef(Boolean(streaming));

  React.useEffect(() => {
    if (streaming) {
      setAccordionValue("upstream-think");
      wasStreamingRef.current = true;
      return;
    }

    if (wasStreamingRef.current && autoCollapseReady) {
      setAccordionValue("");
    }
    if (autoCollapseReady) {
      wasStreamingRef.current = false;
    }
  }, [autoCollapseReady, streaming]);

  if (!block) {
    return null;
  }

  const open = accordionValue === "upstream-think";
  const resolvedTitle = title ?? (streaming ? labels.think.titleActive : labels.think.titleDone);
  const resolvedSubtitle = subtitle ?? (streaming ? labels.think.subtitleActive : labels.think.subtitleDone);
  const contentSegments = block.contentSegments?.filter((item) => item.trim()) ?? [];

  return (
    <div className={TRACE_ROOT_CLASS}>
      <Accordion
        type="single"
        collapsible
        value={accordionValue}
        onValueChange={(value) => setAccordionValue(value || "")}
        className="w-full"
      >
        <AccordionItem value="upstream-think" className="border-b-0">
          <AccordionTrigger
            iconPosition="none"
            className="group items-start justify-between gap-1.5 py-0 text-left no-underline hover:no-underline"
          >
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-1.5">
                <Marker
                  render={<span />}
                  className={cn(
                    "inline-flex min-h-0 w-auto text-[13px] font-medium transition-colors",
                    !streaming && "text-muted-foreground group-hover:text-foreground",
                  )}
                >
                  <MarkerContent className={cn("min-w-0", streaming && "shimmer")}>
                    {resolvedTitle}
                  </MarkerContent>
                </Marker>
              </div>
              <div className="mt-0.5 truncate text-[11px] font-normal leading-4 text-muted-foreground/62">{resolvedSubtitle}</div>
            </div>
            <ChevronDown
              className={cn(
                "mt-0.5 size-3.5 shrink-0 text-muted-foreground transition-transform duration-200 group-hover:text-foreground",
                open && "rotate-180",
              )}
            />
          </AccordionTrigger>
          <AccordionContent className="px-0 pb-0 pt-1.5 duration-[350ms] ease-in-out">
            {contentSegments.length > 0 ? (
              <div className="space-y-3">
                {contentSegments.map((content, index) => (
                  <StreamdownRender
                    key={`${index}-${content.slice(0, 24)}`}
                    content={content}
                    streaming={Boolean(streaming && index === contentSegments.length - 1)}
                    variant="thinking"
                  />
                ))}
              </div>
            ) : (
              <StreamdownRender content={block.contentMarkdown} streaming={Boolean(streaming)} variant="thinking" />
            )}
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    </div>
  );
}
