import { mount } from 'svelte';
import App from './App.svelte';
import '../shared/styles/tokens.css';
import '../shared/styles/base.css';

mount(App, { target: document.getElementById('app')! });
