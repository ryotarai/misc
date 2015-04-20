// ==UserScript==
// @name         Bigger Slides for Speaker Deck
// @namespace    http://ryotarai.info/
// @version      0.1
// @description  like Theater Mode in YouTube
// @author       Ryota Arai
// @match        https://speakerdeck.com/*/*
// @grant        none
// ==/UserScript==

window.onload = function() {
    var sidebar = document.querySelector("div.sidebar");
    if (sidebar) {
        sidebar.style.display = "none";
    }
    
    var iframe = document.querySelector("iframe.speakerdeck-iframe");
    if (iframe) {
        var origWidth = parseFloat(iframe.style.width);
        var origHeight = parseFloat(iframe.style.height);

        var width = 960;
        var height = origHeight * (width / origWidth);

        iframe.style.width = width + "px";
        iframe.style.height = height + "px";
    }
};

