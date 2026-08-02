"use client";

import * as React from "react";
import { useTranslations } from "next-intl";

import { cn } from "@/lib/utils";
import {
  classifyTableColumns,
  type ColumnAnalyzerOptions,
  type ColumnType,
  mergeColumnType,
} from "./markdown-table-analyzer";

const INITIAL_STREAMING_ROWS = 3;
const INITIAL_ANALYSIS_DELAY_MS = 120;
const REANALYSIS_DEBOUNCE_MS = 420;

export const MarkdownTableStreamingContext = React.createContext(false);
export const MarkdownTableAnalyzerOptionsContext = React.createContext<ColumnAnalyzerOptions | undefined>(undefined);

type MarkdownTableProps = React.TableHTMLAttributes<HTMLTableElement> & {
  children?: React.ReactNode;
  node?: unknown;
};

type TableSnapshot = {
  headers: string[];
  rows: string[][];
  columnCount: number;
};

type ElementWithChildren = React.ReactElement<{
  children?: React.ReactNode;
  className?: string;
  node?: unknown;
  scope?: string;
}>;

export function AdaptiveMarkdownTable({ children, className, node: _node, ...props }: MarkdownTableProps) {
  const t = useTranslations("chat.markdown");
  const streaming = React.useContext(MarkdownTableStreamingContext);
  const analyzerOptions = React.useContext(MarkdownTableAnalyzerOptionsContext);
  const snapshot = React.useMemo(() => getTableSnapshot(children), [children]);
  const candidateTypes = React.useMemo(
    () => classifyTableColumns(snapshot.headers, snapshot.rows, analyzerOptions),
    [analyzerOptions, snapshot],
  );
  const columnTypes = useStableColumnTypes(candidateTypes, snapshot.rows.length, streaming);
  const contentColumnCount = columnTypes.filter((type) => type === "content").length;
  const contentColumnBucket = contentColumnCount >= 3 ? "many" : String(contentColumnCount);
  const scrollRef = React.useRef<HTMLDivElement>(null);
  const hintID = React.useId();
  const [hasHorizontalOverflow, setHasHorizontalOverflow] = React.useState(false);
  const [showOverflowHint, setShowOverflowHint] = React.useState(false);

  const updateOverflowHint = React.useCallback(() => {
    const scrollElement = scrollRef.current;
    if (!scrollElement) {
      return;
    }
    const hasHiddenContent = scrollElement.scrollWidth - scrollElement.clientWidth > 2;
    const direction = window.getComputedStyle(scrollElement).direction;
    const inlineProgress = direction === "rtl" ? Math.abs(scrollElement.scrollLeft) : scrollElement.scrollLeft;
    const isAtInlineEnd = inlineProgress + scrollElement.clientWidth >= scrollElement.scrollWidth - 2;
    setHasHorizontalOverflow(hasHiddenContent);
    setShowOverflowHint(hasHiddenContent && !isAtInlineEnd);
  }, []);

  React.useLayoutEffect(() => {
    updateOverflowHint();
    const scrollElement = scrollRef.current;
    if (!scrollElement || typeof ResizeObserver === "undefined") {
      return;
    }
    const observer = new ResizeObserver(updateOverflowHint);
    observer.observe(scrollElement);
    const table = scrollElement.querySelector("table");
    if (table) {
      observer.observe(table);
    }
    return () => observer.disconnect();
  }, [columnTypes, updateOverflowHint]);

  const decoratedChildren = React.useMemo(
    () => decorateTableChildren(children, columnTypes),
    [children, columnTypes],
  );

  return (
    <div
      className="markdown-table-shell"
      data-content-columns={contentColumnBucket}
      data-overflow-hint={showOverflowHint ? "visible" : "hidden"}
    >
      {/* Keyboard users need to focus the horizontal scroll region itself. */}
      <div
        ref={scrollRef}
        aria-describedby={hasHorizontalOverflow ? hintID : undefined}
        aria-label={t("scrollableTable")}
        className="markdown-table-scroll"
        role="region"
        tabIndex={hasHorizontalOverflow ? 0 : undefined}
        onScroll={updateOverflowHint}
      >
        <table {...props} className={cn("markdown-table", className)} data-streamdown="table">
          <colgroup>
            {columnTypes.map((type, index) => (
              <col
                // Column order is dynamic; this key only identifies the current rendered column.
                key={`${index}-${type}`}
                className={`markdown-table-col markdown-table-col--${type}`}
                data-markdown-column-type={type}
              />
            ))}
          </colgroup>
          {decoratedChildren}
        </table>
      </div>
      <span id={hintID} className="sr-only">
        {t("scrollableTableHint")}
      </span>
    </div>
  );
}

function useStableColumnTypes(
  candidateTypes: readonly ColumnType[],
  rowCount: number,
  streaming: boolean,
): ColumnType[] {
  const [streamingTypes, setStreamingTypes] = React.useState<ColumnType[]>(() =>
    Array.from({ length: candidateTypes.length }, () => "normal"),
  );
  const classifiedRef = React.useRef(false);
  const hasStreamedRef = React.useRef(streaming);
  React.useEffect(() => {
    if (!streaming) {
      if (hasStreamedRef.current) {
        setStreamingTypes((previousTypes) =>
          candidateTypes.map((nextType, index) =>
            mergeColumnType(previousTypes[index] ?? "normal", nextType),
          ),
        );
      }
      return;
    }

    if (candidateTypes.length !== streamingTypes.length) {
      classifiedRef.current = false;
      setStreamingTypes(Array.from({ length: candidateTypes.length }, () => "normal"));
    }
    if (rowCount < INITIAL_STREAMING_ROWS) {
      return;
    }

    const delay = classifiedRef.current ? REANALYSIS_DEBOUNCE_MS : INITIAL_ANALYSIS_DELAY_MS;
    const timeout = window.setTimeout(() => {
      setStreamingTypes((previousTypes) => {
        if (!classifiedRef.current || previousTypes.length !== candidateTypes.length) {
          return [...candidateTypes];
        }
        return candidateTypes.map((nextType, index) =>
          mergeColumnType(previousTypes[index] ?? "normal", nextType),
        );
      });
      classifiedRef.current = true;
    }, delay);

    return () => window.clearTimeout(timeout);
  }, [candidateTypes, rowCount, streaming, streamingTypes.length]);

  if (!streaming) {
    if (!hasStreamedRef.current) {
      return [...candidateTypes];
    }
    return candidateTypes.map((nextType, index) =>
      mergeColumnType(streamingTypes[index] ?? "normal", nextType),
    );
  }
  return streamingTypes.length === candidateTypes.length
    ? streamingTypes
    : Array.from({ length: candidateTypes.length }, () => "normal");
}

function getTableSnapshot(children: React.ReactNode): TableSnapshot {
  const sections = React.Children.toArray(children).filter(React.isValidElement) as ElementWithChildren[];
  let headers: string[] = [];
  const rows: string[][] = [];

  for (const section of sections) {
    const sectionRows = getChildElements(section.props.children);
    const tagName = getElementTagName(section);
    for (const row of sectionRows) {
      const values = getChildElements(row.props.children).map((cell) => getReactNodeText(cell.props.children));
      if (tagName === "thead" && headers.length === 0) {
        headers = values;
      } else {
        rows.push(values);
      }
    }
  }

  const columnCount = Math.max(headers.length, ...rows.map((row) => row.length), 0);
  if (headers.length < columnCount) {
    headers = [...headers, ...Array.from({ length: columnCount - headers.length }, () => "")];
  }
  return { headers, rows, columnCount };
}

function decorateTableChildren(children: React.ReactNode, columnTypes: readonly ColumnType[]): React.ReactNode {
  return React.Children.map(children, (sectionNode) => {
    if (!React.isValidElement(sectionNode)) {
      return sectionNode;
    }
    const section = sectionNode as ElementWithChildren;
    const headerSection = getElementTagName(section) === "thead";
    const rows = React.Children.map(section.props.children, (rowNode) => {
      if (!React.isValidElement(rowNode)) {
        return rowNode;
      }
      const row = rowNode as ElementWithChildren;
      const cells = React.Children.map(row.props.children, (cellNode, columnIndex) => {
        if (!React.isValidElement(cellNode)) {
          return cellNode;
        }
        const cell = cellNode as ElementWithChildren;
        const type = columnTypes[columnIndex] ?? "normal";
        return React.cloneElement(cell, {
          className: cn(cell.props.className, "markdown-table-cell", `markdown-table-cell--${type}`),
          "data-markdown-column-type": type,
          ...(headerSection ? { scope: "col" } : {}),
        } as React.HTMLAttributes<HTMLTableCellElement>);
      });
      return React.cloneElement(row, undefined, cells);
    });
    return React.cloneElement(section, undefined, rows);
  });
}

function getChildElements(children: React.ReactNode): ElementWithChildren[] {
  return React.Children.toArray(children).filter(React.isValidElement) as ElementWithChildren[];
}

function getElementTagName(element: ElementWithChildren): string {
  const node = element.props.node as { tagName?: string } | undefined;
  if (node?.tagName) {
    return node.tagName.toLowerCase();
  }
  if (typeof element.type === "string") {
    return element.type.toLowerCase();
  }
  const displayName = (element.type as React.ComponentType).displayName ?? (element.type as React.ComponentType).name ?? "";
  return displayName.toLowerCase().replace(/^markdown/, "");
}

function getReactNodeText(node: React.ReactNode): string {
  return React.Children.toArray(node)
    .map((child) => {
      if (typeof child === "string" || typeof child === "number") {
        return String(child);
      }
      if (React.isValidElement<{ children?: React.ReactNode }>(child)) {
        return getReactNodeText(child.props.children);
      }
      return "";
    })
    .join("")
    .trim();
}
