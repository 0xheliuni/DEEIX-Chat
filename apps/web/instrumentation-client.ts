// Runs once in the browser before hydration (Next.js client instrumentation).

// Dev only: React 19's RSC performance tracks call performance.measure() with
// ranges WebKit rejects (end < start → TypeError), which trips the Next error
// overlay on every navigation in Safari and the desktop webview. Chrome
// tolerates the same call. Swallow just that error; production builds do not
// contain the tracking code at all.
if (process.env.NODE_ENV === "development" && typeof performance !== "undefined") {
  const measure = performance.measure.bind(performance);
  performance.measure = ((...args: Parameters<typeof measure>) => {
    try {
      return measure(...args);
    } catch (error) {
      if (error instanceof TypeError) {
        return undefined as unknown as PerformanceMeasure;
      }
      throw error;
    }
  }) as typeof performance.measure;
}
