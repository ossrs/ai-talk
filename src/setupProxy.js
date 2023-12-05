//
// Copyright (c) 2022-2023 Winlin
//
// SPDX-License-Identifier: AGPL-3.0-or-later
//
const { createProxyMiddleware } = require('http-proxy-middleware');

console.log('setupProxy for development reactjs');

// See https://create-react-app.dev/docs/proxying-api-requests-in-development/
// See https://create-react-app.dev/docs/proxying-api-requests-in-development/#configuring-the-proxy-manually
module.exports = function(app) {
  app.use('/api/gpt-microphone/upload/', createProxyMiddleware({target: 'http://127.0.0.1:3001/'}));
};

