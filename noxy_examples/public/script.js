console.log("Script loaded from Noxy server!");

document.getElementById('btn').addEventListener('click', () => {
    alert('Button clicked! JS is working.');
    document.getElementById('output').innerText = 'You clicked the button at ' + new Date().toLocaleTimeString();
});
