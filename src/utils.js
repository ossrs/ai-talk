
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
