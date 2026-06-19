import React from 'react';
import {createRoot} from 'react-dom/client';
import './styles.css';
import App from './App';
import {prefetchAndroidVersionMap} from './lib/android';

prefetchAndroidVersionMap();

const root = createRoot(document.getElementById('root')!);
root.render(
  <React.StrictMode>
    <App/>
  </React.StrictMode>
);
