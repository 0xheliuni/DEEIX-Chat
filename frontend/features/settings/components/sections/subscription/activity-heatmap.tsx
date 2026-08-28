"use client";

import { Flame } from "lucide-react";
import { useTranslations } from "next-intl";
import * as React from "react";
import { toast } from "sonner";

import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { formatTokenCount } from "@/features/settings/model/subscription-format";
import { useAppLocale } from "@/i18n/app-i18n-provider";
import { useLocalizedErrorMessage } from "@/i18n/use-localized-error";
import { getUserActivity } from "@/shared/api/stats";
import type { UserActivityDailyDTO } from "@/shared/api/stats.types";
import { ActivityHeatmapSkeleton } from "./activity-heatmap-skeleton";

export type ActivityMetric = "tokens" | "messages";

type ActivityDay = {
  date: string;
  messages: number;
  tokens: number;
};

type ActivityStats = {
  peakDayTokens: number;
  currentStreak: number;
  longestStreak: number;
  activeDays: number;
  totalMessages: number;
};

type HeatmapWeek = Array<ActivityDay | null>;

const ACTIVITY_DAYS = 365;
const HEATMAP_MAX_LEVEL = 4;
const MESSAGES_PER_LEVEL = 5;

const HEATMAP_CELL_CLASS = [
  "bg-muted",
  "bg-green-600/25 dark:bg-green-500/25",
  "bg-green-600/45 dark:bg-green-500/45",
  "bg-green-600/70 dark:bg-green-500/70",
  "bg-green-600 dark:bg-green-500",
] as const;

// 与 LobeHub 的分档一致：tokens 相对峰值归一化，消息数固定步进。
function resolveLevel(value: number, isTokenMetric: boolean, peakTokens: number): number {
  if (value <= 0) return 0;
  const level = isTokenMetric
    ? Math.ceil((value / peakTokens) * HEATMAP_MAX_LEVEL)
    : Math.ceil(value / MESSAGES_PER_LEVEL);
  return Math.min(HEATMAP_MAX_LEVEL, Math.max(1, level));
}

function parseActivityDay(value: UserActivityDailyDTO): ActivityDay {
  return {
    date: value.date,
    messages: Number(value.messageCount) || 0,
    tokens: Number(value.tokenUsage) || 0,
  };
}

function sortActivityDays(items: UserActivityDailyDTO[]): ActivityDay[] {
  return items.map(parseActivityDay).sort((left, right) => left.date.localeCompare(right.date));
}

// GitHub 风格周列：第一列按周日对齐，不足的格子前置补空。
function buildHeatmapWeeks(days: ActivityDay[]): HeatmapWeek[] {
  if (days.length === 0) return [];
  const leadingEmpty = new Date(`${days[0].date}T00:00:00`).getDay();
  const cells: Array<ActivityDay | null> = [...Array.from({ length: leadingEmpty }, () => null), ...days];
  const weeks: HeatmapWeek[] = [];
  for (let index = 0; index < cells.length; index += 7) {
    const week = cells.slice(index, index + 7);
    while (week.length < 7) week.push(null);
    weeks.push(week);
  }
  return weeks;
}

// 某列包含当月 1 日时显示月份标签；一个月的 1 日只可能落在一列。
function buildMonthLabels(weeks: HeatmapWeek[], locale: string): string[] {
  const formatter = new Intl.DateTimeFormat(locale, { month: "short" });
  return weeks.map((week) => {
    const firstDay = week.find((cell): cell is ActivityDay => Boolean(cell) && new Date(`${cell!.date}T00:00:00`).getDate() === 1);
    return firstDay ? formatter.format(new Date(`${firstDay.date}T00:00:00`)) : "";
  });
}

function computeActivityStats(days: ActivityDay[]): ActivityStats {
  let peakDayTokens = 0;
  let activeDays = 0;
  let totalMessages = 0;
  let longestStreak = 0;
  let runningStreak = 0;
  for (const day of days) {
    peakDayTokens = Math.max(peakDayTokens, day.tokens);
    totalMessages += day.messages;
    if (day.messages > 0) {
      activeDays += 1;
      runningStreak += 1;
      longestStreak = Math.max(longestStreak, runningStreak);
    } else {
      runningStreak = 0;
    }
  }
  // 今日尚未结束：末尾为 0 时从昨天起算连续天数。
  let cursor = days.length - 1;
  if (cursor >= 0 && days[cursor].messages === 0) cursor -= 1;
  let currentStreak = 0;
  while (cursor >= 0 && days[cursor].messages > 0) {
    currentStreak += 1;
    cursor -= 1;
  }
  return { peakDayTokens, currentStreak, longestStreak, activeDays, totalMessages };
}

function formatActivityDate(date: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(new Date(`${date}T00:00:00`));
}

function ActivityMetricTile({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-md bg-muted/40 px-3 py-3.5 md:px-4">
      <p className="truncate text-xs text-muted-foreground">{label}</p>
      <p className="mt-2 truncate text-base font-semibold tabular-nums text-foreground md:text-lg">{value}</p>
    </div>
  );
}

function HeatmapTooltipContent({ day, locale }: { day: ActivityDay; locale: string }) {
  const t = useTranslations("settings.subscriptionPage.activity.tooltip");
  return (
    <div className="grid min-w-[8rem] gap-1.5 rounded-md border border-border/50 bg-background px-2.5 py-2 text-xs text-foreground shadow-md">
      <p className="font-medium">{formatActivityDate(day.date, locale)}</p>
      <div className="grid gap-1 text-muted-foreground">
        <div className="flex items-center justify-between gap-6">
          <span>{t("messages")}</span>
          <span className="font-medium tabular-nums text-foreground">{day.messages.toLocaleString("en-US")}</span>
        </div>
        <div className="flex items-center justify-between gap-6">
          <span>{t("tokens")}</span>
          <span className="font-medium tabular-nums text-foreground">{formatTokenCount(day.tokens)}</span>
        </div>
      </div>
    </div>
  );
}

export function SubscriptionActivityHeatmap({ accessToken }: { accessToken: string }) {
  const t = useTranslations("settings.subscriptionPage.activity");
  const resolveErrorMessage = useLocalizedErrorMessage();
  const { locale } = useAppLocale();
  const [metric, setMetric] = React.useState<ActivityMetric>("tokens");
  const [days, setDays] = React.useState<ActivityDay[] | null>(null);

  React.useEffect(() => {
    let mounted = true;
    getUserActivity(accessToken, { days: ACTIVITY_DAYS })
      .then((items) => {
        if (mounted) setDays(sortActivityDays(items ?? []));
      })
      .catch((error) => {
        if (mounted) {
          toast.error(t("loadFailed"), { description: resolveErrorMessage(error, t("loadFailed")) });
          setDays([]);
        }
      });
    return () => {
      mounted = false;
    };
  }, [accessToken, resolveErrorMessage, t]);

  const weeks = React.useMemo(() => buildHeatmapWeeks(days ?? []), [days]);
  const monthLabels = React.useMemo(() => buildMonthLabels(weeks, locale), [locale, weeks]);
  const stats = React.useMemo(() => computeActivityStats(days ?? []), [days]);

  if (days === null) {
    return <ActivityHeatmapSkeleton />;
  }

  const metricValue = (day: ActivityDay) => (metric === "tokens" ? day.tokens : day.messages);
  const hasAnyActivity = stats.totalMessages > 0 || stats.peakDayTokens > 0;

  return (
    <div className="space-y-3 rounded-md bg-muted/35 p-3">
      <div className="flex h-9 items-center justify-between gap-3 px-1">
        <div className="flex min-w-0 items-center gap-2">
          <h3 className="truncate text-sm font-semibold">{t("title")}</h3>
          {stats.activeDays > 0 ? (
            <span className="flex shrink-0 items-center gap-1 rounded-md bg-green-600/10 px-1.5 py-0.5 text-xs font-medium text-green-700 dark:text-green-400">
              <Flame aria-hidden className="size-3" />
              {t("activeDays", { count: stats.activeDays })}
            </span>
          ) : null}
        </div>
        <Tabs value={metric} onValueChange={(value) => setMetric(value as ActivityMetric)}>
          <TabsList>
            <TabsTrigger value="tokens">{t("tokens")}</TabsTrigger>
            <TabsTrigger value="messages">{t("messages")}</TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
        <ActivityMetricTile label={t("peakDayTokens")} value={formatTokenCount(stats.peakDayTokens)} />
        <ActivityMetricTile label={t("currentStreak")} value={`${stats.currentStreak}`} />
        <ActivityMetricTile label={t("longestStreak")} value={`${stats.longestStreak}`} />
        <ActivityMetricTile label={t("totalActiveDays")} value={`${stats.activeDays}`} />
      </div>

      {!hasAnyActivity ? (
        <div className="flex h-[104px] items-center justify-center text-xs text-muted-foreground">{t("empty")}</div>
      ) : (
        <div className="overflow-x-auto pb-1">
          {/* GitHub 风格：周列 flex-1 等宽铺满卡片，格子 aspect-square 随宽度伸缩；窄屏保持最小可读宽度横向滚动。
              注意不能用 grid-auto-flow:column + 隐式行（Chromium 会把格子的百分比/宽高比尺寸解析成 0）。 */}
          <div className="min-w-[640px] space-y-1">
            <div className="flex gap-[3px] text-[10px] leading-none text-muted-foreground">
              {monthLabels.map((label, index) => (
                <span key={`activity-month-${index}`} className="min-w-0 flex-1 whitespace-nowrap">
                  {label}
                </span>
              ))}
            </div>
            <div className="flex gap-[3px]">
              {weeks.map((week, weekIndex) => (
                <div key={`activity-week-${weekIndex}`} className="flex min-w-0 flex-1 flex-col gap-[3px]">
                  {week.map((day, dayIndex) => {
                    if (!day) {
                      return <div key={`activity-empty-${weekIndex}-${dayIndex}`} className="aspect-square w-full rounded-[2px] bg-transparent" />;
                    }
                    const value = metricValue(day);
                    const level = resolveLevel(value, metric === "tokens", stats.peakDayTokens);
                    return (
                      <Tooltip key={`activity-cell-${day.date}`}>
                        <TooltipTrigger asChild>
                          <span
                            aria-label={formatActivityDate(day.date, locale)}
                            className={`aspect-square w-full cursor-default rounded-[2px] ${HEATMAP_CELL_CLASS[level]}`}
                          />
                        </TooltipTrigger>
                        {/* 覆盖默认反色主题，避免与内部浅色卡片叠放后出现隐形文字。 */}
                        <TooltipContent className="border-border/50 bg-background p-0 text-foreground shadow-md">
                          <HeatmapTooltipContent day={day} locale={locale} />
                        </TooltipContent>
                      </Tooltip>
                    );
                  })}
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

      <div className="flex flex-wrap items-center justify-between gap-2 px-1 text-xs text-muted-foreground">
        <span>{t("totalMessages", { count: stats.totalMessages })}</span>
        <span className="flex items-center gap-1">
          {t("less")}
          {HEATMAP_CELL_CLASS.map((cellClass, level) => (
            <span key={`activity-legend-${level}`} className={`size-2.5 rounded-[2px] ${cellClass}`} />
          ))}
          {t("more")}
        </span>
      </div>
    </div>
  );
}
