"use client";

import { useTranslations } from "next-intl";
import * as React from "react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  type ContentModerationConfig,
  getContentModerationConfig,
  probeContentModeration,
  updateContentModerationConfig,
} from "@/features/admin/api/content-moderation";
import { useAuthSession } from "@/shared/auth/auth-session-context";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";

const TEXT_DEFAULTS = [
  "hate",
  "hate/threatening",
  "harassment",
  "harassment/threatening",
  "self-harm",
  "self-harm/intent",
  "self-harm/instructions",
  "sexual",
  "sexual/minors",
  "violence",
  "violence/graphic",
  "illicit",
  "illicit/violent",
];

const IMAGE_DEFAULTS = [
  "self-harm",
  "self-harm/intent",
  "self-harm/instructions",
  "sexual",
  "violence",
  "violence/graphic",
];

function CategoryChecklist({
  options,
  value,
  onChange,
  selectAllLabel,
  disabled,
}: {
  options: string[];
  value: string[];
  onChange: (next: string[]) => void;
  selectAllLabel: string;
  disabled?: boolean;
}) {
  const selected = new Set(value);
  const selectedOptionCount = options.filter((category) => selected.has(category)).length;
  const allSelected = options.length > 0 && selectedOptionCount === options.length;
  return (
    <div className="space-y-3">
      <label className="flex w-fit items-center gap-2 text-sm font-medium">
        <Checkbox
          checked={allSelected ? true : selectedOptionCount > 0 ? "indeterminate" : false}
          disabled={disabled || options.length === 0}
          onCheckedChange={(state) => {
            onChange(state === true ? Array.from(new Set(options)).sort() : []);
          }}
        />
        <span>{selectAllLabel}</span>
      </label>
      <div className="grid gap-2 sm:grid-cols-2">
        {options.map((category) => {
          const checked = selected.has(category);
          return (
            <label key={category} className="flex items-center gap-2 text-sm">
              <Checkbox
                checked={checked}
                disabled={disabled}
                onCheckedChange={(state) => {
                  const next = new Set(selected);
                  if (state) next.add(category);
                  else next.delete(category);
                  onChange(Array.from(next).sort());
                }}
              />
              <span className="font-mono text-xs">{category}</span>
            </label>
          );
        })}
      </div>
    </div>
  );
}

export function AdminContentModeration() {
  const t = useTranslations("adminContentModeration");
  const { user } = useAuthSession();
  const isSuperAdmin = user?.role === "superadmin";
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);
  const [probing, setProbing] = React.useState(false);
  const [config, setConfig] = React.useState<ContentModerationConfig | null>(null);
  const [textCategories, setTextCategories] = React.useState<string[]>(TEXT_DEFAULTS);
  const [imageCategories, setImageCategories] = React.useState<string[]>(IMAGE_DEFAULTS);
  const [apiKeyDraft, setApiKeyDraft] = React.useState("");
  const [probeSummary, setProbeSummary] = React.useState<string>("");

  const load = React.useCallback(async () => {
    setLoading(true);
    try {
      const token = await resolveAccessToken();
      if (!token) return;
      if (!isSuperAdmin) return;
      const cfgRes = await getContentModerationConfig(token);
      setConfig(cfgRes.config);
      setTextCategories(cfgRes.categories?.text?.length ? cfgRes.categories.text : TEXT_DEFAULTS);
      setImageCategories(cfgRes.categories?.image?.length ? cfgRes.categories.image : IMAGE_DEFAULTS);
    } catch {
      toast.error(t("loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [isSuperAdmin, t]);

  React.useEffect(() => {
    void load();
  }, [load]);

  const save = async () => {
    if (!config || !isSuperAdmin) return;
    setSaving(true);
    try {
      const token = await resolveAccessToken();
      if (!token) return;
      const res = await updateContentModerationConfig(token, {
        baseUrl: config.baseUrl,
        model: config.model,
        timeoutSeconds: config.timeoutSeconds,
        maxConcurrency: config.maxConcurrency,
        queueCapacity: config.queueCapacity,
        apiKey: apiKeyDraft.trim() || undefined,
        policy: config.policy,
      });
      setConfig(res.config);
      setApiKeyDraft("");
      toast.success(t("saved"));
    } catch {
      toast.error(t("saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const probe = async () => {
    if (!isSuperAdmin) return;
    setProbing(true);
    try {
      const token = await resolveAccessToken();
      if (!token) return;
      const res = await probeContentModeration(token);
      setProbeSummary(
        `${t("probeText")}: ${res.text.valid ? t("valid") : t("invalid")} (${res.text.latencyMS}ms)` +
          ` · ${t("probeImage")}: ${res.image.valid ? t("valid") : t("invalid")} (${res.image.latencyMS}ms)`,
      );
    } catch {
      toast.error(t("probeFailed"));
    } finally {
      setProbing(false);
    }
  };

  if (loading) {
    return <div className="text-sm text-muted-foreground">{t("loading")}</div>;
  }

  if (!isSuperAdmin) {
    return (
      <div className="space-y-3 pb-10">
        <div className="flex h-10 items-center px-1">
          <h3 className="text-sm font-semibold">{t("title")}</h3>
        </div>
        <p className="px-1 text-sm text-muted-foreground">{t("superAdminOnly")}</p>
      </div>
    );
  }

  if (!config) {
    return (
      <div className="space-y-3 pb-10">
        <div className="flex h-10 items-center px-1">
          <h3 className="text-sm font-semibold">{t("title")}</h3>
        </div>
        <p className="px-1 text-sm text-muted-foreground">{t("loadFailed")}</p>
      </div>
    );
  }

  return (
    <div className="space-y-3 pb-10">
      <div className="flex h-10 items-center px-1">
        <h3 className="text-sm font-semibold">{t("title")}</h3>
      </div>

      <Tabs defaultValue="policy">
        <TabsList className="flex h-auto flex-wrap">
          <TabsTrigger value="policy">{t("tabs.policy")}</TabsTrigger>
          <TabsTrigger value="service">{t("tabs.service")}</TabsTrigger>
        </TabsList>

        <TabsContent value="policy" className="space-y-6 pt-4">
          <p className="text-sm text-muted-foreground">{t("policyHint")}</p>
          {(
            [
              ["inputText", "inputTextCategories", textCategories],
              ["inputImage", "inputImageCategories", imageCategories],
              ["outputText", "outputTextCategories", textCategories],
              ["outputImage", "outputImageCategories", imageCategories],
            ] as const
          ).map(([labelKey, field, options]) => (
            <section key={field} className="space-y-3 rounded-md border border-border/60 p-4">
              <h4 className="text-sm font-medium">{t(`surfaces.${labelKey}`)}</h4>
              <CategoryChecklist
                options={options}
                value={config.policy[field] ?? []}
                selectAllLabel={t("selectAll")}
                onChange={(next) =>
                  setConfig({
                    ...config,
                    policy: { ...config.policy, [field]: next },
                  })
                }
              />
            </section>
          ))}
          <Button onClick={() => void save()} disabled={saving}>
            {saving ? t("saving") : t("save")}
          </Button>
        </TabsContent>

        <TabsContent value="service" className="space-y-4 pt-4">
          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <Label>{t("fields.baseUrl")}</Label>
              <Input
                value={config.baseUrl}
                onChange={(e) => setConfig({ ...config, baseUrl: e.target.value })}
              />
            </div>
            <div className="space-y-2">
              <Label>{t("fields.model")}</Label>
              <Input
                value={config.model}
                onChange={(e) => setConfig({ ...config, model: e.target.value })}
              />
            </div>
            <div className="space-y-2">
              <Label>{t("fields.apiKey")}</Label>
              <Input
                type="password"
                placeholder={config.hasAPIKey ? config.apiKeyMasked || "••••" : t("fields.apiKeyPlaceholder")}
                value={apiKeyDraft}
                onChange={(e) => setApiKeyDraft(e.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label>{t("fields.timeoutSeconds")}</Label>
              <Input
                type="number"
                min={1}
                max={60}
                value={config.timeoutSeconds}
                onChange={(e) => setConfig({ ...config, timeoutSeconds: Number(e.target.value) || 10 })}
              />
            </div>
            <div className="space-y-2">
              <Label>{t("fields.maxConcurrency")}</Label>
              <Input
                type="number"
                min={1}
                max={64}
                value={config.maxConcurrency}
                onChange={(e) => setConfig({ ...config, maxConcurrency: Number(e.target.value) || 4 })}
              />
            </div>
            <div className="space-y-2">
              <Label>{t("fields.queueCapacity")}</Label>
              <Input
                type="number"
                min={1}
                max={4096}
                value={config.queueCapacity}
                onChange={(e) => setConfig({ ...config, queueCapacity: Number(e.target.value) || 256 })}
              />
            </div>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button onClick={() => void save()} disabled={saving}>
              {saving ? t("saving") : t("save")}
            </Button>
            <Button variant="outline" onClick={() => void probe()} disabled={probing}>
              {probing ? t("probing") : t("probe")}
            </Button>
          </div>
          {probeSummary ? <p className="text-sm text-muted-foreground">{probeSummary}</p> : null}
        </TabsContent>
      </Tabs>
    </div>
  );
}
