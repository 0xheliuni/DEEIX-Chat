"use client";

import { useTranslations } from "next-intl";

import { IdentityProviderIcon } from "@/entities/identity-provider";
import { AboutSettingsContent } from "@/shared/components/about-settings-content";

export function SettingsAbout() {
  const t = useTranslations("settings.aboutPage");

  return (
    <AboutSettingsContent
      brandIcon={IdentityProviderIcon}
      title={t("title")}
      description={t("description")}
      consoleLabel={t("userConsole")}
      labels={{
        details: t("details"),
        official: t("official"),
        website: t("website"),
        repository: t("repository"),
        social: t("social"),
        blog: t("blog"),
        contact: t("contact"),
        copyright: t("copyright"),
        license: t("license"),
      }}
    />
  );
}
