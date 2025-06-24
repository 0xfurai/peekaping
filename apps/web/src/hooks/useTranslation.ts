import { useTranslation } from 'react-i18next';

export const useLocalizedTranslation = () => {
  const { t, i18n } = useTranslation();

  const changeLanguage = (language: string) => {
    i18n.changeLanguage(language);
  };

  const getCurrentLanguage = () => i18n.language;

  const getAvailableLanguages = () => Object.keys(i18n.services.resourceStore.data);

  return {
    t,
    changeLanguage,
    getCurrentLanguage,
    getAvailableLanguages,
    i18n,
  };
};

// Convenience hooks for specific domains
export const useCommonTranslations = () => {
  const { t } = useLocalizedTranslation();
  return {
    loading: t('common.loading'),
    save: t('common.save'),
    cancel: t('common.cancel'),
    delete: t('common.delete'),
    edit: t('common.edit'),
    create: t('common.create'),
    update: t('common.update'),
    search: t('common.search'),
    filter: t('common.filter'),
    all: t('common.all'),
    active: t('common.active'),
    inactive: t('common.inactive'),
    up: t('common.up'),
    down: t('common.down'),
    unknown: t('common.unknown'),
    maintenance: t('common.maintenance'),
    pending: t('common.pending'),
    yes: t('common.yes'),
    no: t('common.no'),
  };
};

export const useMonitorTranslations = () => {
  const { t } = useLocalizedTranslation();
  return {
    title: t('monitors.title'),
    friendlyName: t('monitors.friendly_name'),
    monitorType: t('monitors.monitor_type'),
    heartbeatInterval: t('monitors.heartbeat_interval'),
    retries: t('monitors.retries'),
    liveStatus: t('monitors.live_status'),
    checkEvery: (interval: number) => t('monitors.check_every', { interval }),
    importantNotifications: t('monitors.important_notifications'),
    noImportantNotifications: t('monitors.no_important_notifications'),
    filters: {
      searchPlaceholder: t('monitors.filters.search_placeholder'),
      monitorStatus: t('monitors.filters.monitor_status'),
      monitorType: t('monitors.filters.monitor_type'),
      statusFilter: t('monitors.filters.status_filter'),
    },
    placeholders: {
      friendlyName: t('monitors.placeholders.friendly_name'),
      monitorType: t('monitors.placeholders.monitor_type'),
      searchMonitors: t('monitors.filters.search_placeholder'),
    },
  };
};

export const useNotificationTranslations = () => {
  const { t } = useLocalizedTranslation();
  return {
    title: t('notifications.title'),
    addNotifier: t('notifications.add_notifier'),
    newNotifier: t('notifications.new_notifier'),
    selectNotifier: t('notifications.select_notifier'),
    selectNotificationChannel: t('notifications.select_notification_channel'),
    noNotificationChannels: t('notifications.no_notification_channels'),
    removeNotification: (name: string) => t('notifications.remove_notification', { name }),
  };
};

export const useStatusTranslations = () => {
  const { t } = useLocalizedTranslation();
  return {
    up: t('common.up'),
    down: t('common.down'),
    unknown: t('common.unknown'),
    maintenance: t('common.maintenance'),
    pending: t('common.pending'),
    getStatusText: (status: number) => {
      switch (status) {
        case 0:
          return t('common.down');
        case 1:
          return t('common.up');
        case 2:
          return t('common.unknown');
        case 3:
          return t('common.maintenance');
        default:
          return t('common.unknown');
      }
    },
  };
};

export const useStatsTranslations = () => {
  const { t } = useLocalizedTranslation();
  return {
    uptime: t('stats.uptime'),
    responseTime: t('stats.response_time'),
    ping: (value: number) => t('stats.ping', { value }),
    retries: (value: number) => t('stats.retries', { value }),
    downCount: (value: number) => t('stats.down_count', { value }),
    notified: (value: string) => t('stats.notified', { value }),
    interval: (value: number) => t('stats.interval', { value }),
  };
};
