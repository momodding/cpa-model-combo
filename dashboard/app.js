applyHostTheme();
observeHostTheme();
initializeCustomRange();
applyUsagePreferences({});
initializeChartInteractions();
initializeDashboardResizeObserver();
initializeConfigurationActions();
scheduleMetricFit();
tabs.forEach(tab=>{tab.addEventListener('click',()=>activateTab(tabName(tab)));tab.addEventListener('keydown',handleTabKey)});
initializeConfigurationEvents();
initializeUsageEvents();
initializeUsageTableEvents();
initializeUsageChartEvents();
initializePricingEvents();
initializeQuotaEvents();
document.addEventListener('visibilitychange',()=>{if(quotaState.active){if(document.hidden){window.clearTimeout(quotaState.timer);quotaState.generation++;if(quotaState.controller)quotaState.controller.abort()}else loadQuotaStatus()}if(!usageState.active)return;if(document.hidden)stopUsagePolling();else refreshUsage(false)});
window.addEventListener('resize',()=>{updateConfigurationActionsClearance();window.clearTimeout(usageState.resizeTimer);usageState.resizeTimer=window.setTimeout(scheduleResponsiveDashboardRender,120)});
document.addEventListener('keydown',event=>{
  if((event.ctrlKey||event.metaKey)&&event.key.toLowerCase()==='s'&&!workspaceEl.hidden&&(!configurationPanelEl.hidden||!quotaPanelEl.hidden)){event.preventDefault();if(state.dirty)saveConfiguration()}
});
if(managementKey())loadConfiguration();
else showFallbackKeyInput('CPAMC has no saved management key. Enter it for this browser session.');

window.addEventListener('pagehide',stopQuotaPolling);
