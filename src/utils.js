import React from 'react';

const AIT_ROBOT_CONFIG = "AIT_ROBOT_CONFIG";

export const RobotConfig = {
  save: (data) => {
    localStorage.setItem(AIT_ROBOT_CONFIG, JSON.stringify(data));
  },
  load: () => {
    const info = localStorage.getItem(AIT_ROBOT_CONFIG);
    if (!info) return null;
    return JSON.parse(info);
  },
  remove: () => {
    localStorage.removeItem(AIT_ROBOT_CONFIG);
  },
};

export function useIsMobile() {
  const [isMobile, setIsMobile] = React.useState(false);

  function handleWindowSizeChange() {
    setIsMobile(window.innerWidth <= 768);
  }
  React.useEffect(() => {
    handleWindowSizeChange();
    window.addEventListener('resize', handleWindowSizeChange);
    return () => {
      window.removeEventListener('resize', handleWindowSizeChange);
    }
  }, [setIsMobile]);

  return isMobile;
}

export function useIsOssrsNet() {
  const [isOssrsNet, setIsOssrsNet] = React.useState(false);
  React.useEffect(() => {
    if (window.location.hostname.indexOf('ossrs.net') >= 0) {
      setIsOssrsNet(true);
    }
  }, [setIsOssrsNet]);
  return isOssrsNet;
}

export function buildTimeString() {
  const date = new Date();
  const hours = date.getHours().toString().padStart(2, '0');
  const minutes = date.getMinutes().toString().padStart(2, '0');
  const seconds = date.getSeconds().toString().padStart(2, '0');

  return `${hours}:${minutes}:${seconds}`;
}

export function buildLog(msg) {
  const log = `[${buildTimeString()}]: ${msg}`;
  console.log(log);
  return log;
}
