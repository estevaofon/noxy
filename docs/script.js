// Mobile navigation toggle
const hamburger = document.getElementById('hamburger');
const navMenu = document.getElementById('nav-menu');

hamburger.addEventListener('click', () => {
    hamburger.classList.toggle('active');
    navMenu.classList.toggle('active');
});

// Close mobile menu when clicking on a link
document.querySelectorAll('.nav-link').forEach(link => {
    link.addEventListener('click', () => {
        hamburger.classList.remove('active');
        navMenu.classList.remove('active');
    });
});

// Smooth scrolling for navigation links
document.querySelectorAll('a[href^="#"]').forEach(anchor => {
    anchor.addEventListener('click', function (e) {
        e.preventDefault();
        const target = document.querySelector(this.getAttribute('href'));
        if (target) {
            target.scrollIntoView({
                behavior: 'smooth',
                block: 'start'
            });
        }
    });
});

// Navbar background on scroll
window.addEventListener('scroll', () => {
    const navbar = document.querySelector('.navbar');
    if (window.scrollY > 50) {
        navbar.style.background = 'rgba(0, 0, 0, 0.98)';
        navbar.style.boxShadow = '0 2px 20px rgba(0, 0, 0, 0.5)';
    } else {
        navbar.style.background = 'rgba(0, 0, 0, 0.95)';
        navbar.style.boxShadow = 'none';
    }
});

// Examples tabs functionality
const tabButtons = document.querySelectorAll('.tab-btn');
const exampleTabs = document.querySelectorAll('.example-tab');

tabButtons.forEach(button => {
    button.addEventListener('click', () => {
        const targetTab = button.getAttribute('data-tab');
        
        // Remove active class from all buttons and tabs
        tabButtons.forEach(btn => btn.classList.remove('active'));
        exampleTabs.forEach(tab => tab.classList.remove('active'));
        
        // Add active class to clicked button and corresponding tab
        button.classList.add('active');
        document.getElementById(targetTab).classList.add('active');
    });
});

// Intersection Observer for animations
const observerOptions = {
    threshold: 0.1,
    rootMargin: '0px 0px -50px 0px'
};

const observer = new IntersectionObserver((entries) => {
    entries.forEach(entry => {
        if (entry.isIntersecting) {
            entry.target.style.opacity = '1';
            entry.target.style.transform = 'translateY(0)';
        }
    });
}, observerOptions);

// Observe elements for animation
document.addEventListener('DOMContentLoaded', () => {
    const animateElements = document.querySelectorAll('.feature-card, .syntax-card, .installation-card');
    
    animateElements.forEach(el => {
        el.style.opacity = '0';
        el.style.transform = 'translateY(30px)';
        el.style.transition = 'opacity 0.6s ease, transform 0.6s ease';
        observer.observe(el);
    });
});

// Copy code functionality
function addCopyButtons() {
    const codeBlocks = document.querySelectorAll('pre code');
    
    codeBlocks.forEach(codeBlock => {
        const pre = codeBlock.parentElement;
        const copyButton = document.createElement('button');
        copyButton.className = 'copy-btn';
        copyButton.innerHTML = '<i class="fas fa-copy"></i>';
        copyButton.title = 'Copiar código';
        
        copyButton.addEventListener('click', async () => {
            try {
                await navigator.clipboard.writeText(codeBlock.textContent);
                copyButton.innerHTML = '<i class="fas fa-check"></i>';
                copyButton.style.color = '#10b981';
                
                setTimeout(() => {
                    copyButton.innerHTML = '<i class="fas fa-copy"></i>';
                    copyButton.style.color = '';
                }, 2000);
            } catch (err) {
                console.error('Erro ao copiar código:', err);
            }
        });
        
        pre.style.position = 'relative';
        pre.appendChild(copyButton);
    });
}

// Initialize copy buttons when DOM is loaded
document.addEventListener('DOMContentLoaded', () => {
    addCopyButtons();
});

// Typing animation for hero code
function typeWriter(element, text, speed = 50) {
    let i = 0;
    element.textContent = '';
    
    function type() {
        if (i < text.length) {
            element.textContent += text.charAt(i);
            i++;
            setTimeout(type, speed);
        }
    }
    
    type();
}

// Easter egg: Konami code
let konamiCode = '';
const konamiSequence = 'ArrowUpArrowUpArrowDownArrowDownArrowLeftArrowRightArrowLeftArrowRightKeyBKeyA';

document.addEventListener('keydown', (e) => {
    konamiCode += e.code;
    
    if (konamiSequence.indexOf(konamiCode) !== 0) {
        konamiCode = '';
    }
    
    if (konamiCode === konamiSequence) {
        showEasterEgg();
        konamiCode = '';
    }
});

function showEasterEgg() {
    const owlEmoji = '🦉';
    const colors = ['#8b5cf6', '#6366f1', '#06b6d4', '#10b981', '#f59e0b'];
    
    for (let i = 0; i < 20; i++) {
        setTimeout(() => {
            createFloatingOwl(owlEmoji, colors[Math.floor(Math.random() * colors.length)]);
        }, i * 100);
    }
}

function createFloatingOwl(emoji, color) {
    const owl = document.createElement('div');
    owl.textContent = emoji;
    owl.style.cssText = `
        position: fixed;
        font-size: 3rem;
        color: ${color};
        pointer-events: none;
        z-index: 9999;
        left: ${Math.random() * window.innerWidth}px;
        top: ${window.innerHeight}px;
        animation: floatUp 3s ease-out forwards;
    `;
    
    document.body.appendChild(owl);
    
    setTimeout(() => {
        owl.remove();
    }, 3000);
}

// Add CSS for floating animation
const style = document.createElement('style');
style.textContent = `
    @keyframes floatUp {
        to {
            transform: translateY(-${window.innerHeight + 200}px) rotate(360deg);
            opacity: 0;
        }
    }
    
    .copy-btn {
        position: absolute;
        top: 1rem;
        right: 1rem;
        background: rgba(255, 255, 255, 0.1);
        border: none;
        color: #e2e8f0;
        padding: 0.5rem;
        border-radius: 4px;
        cursor: pointer;
        transition: all 0.3s ease;
        opacity: 0;
    }
    
    pre:hover .copy-btn {
        opacity: 1;
    }
    
    .copy-btn:hover {
        background: rgba(255, 255, 255, 0.2);
        transform: scale(1.1);
    }
`;
document.head.appendChild(style);

// Performance optimization: Lazy load images
function lazyLoadImages() {
    const images = document.querySelectorAll('img[data-src]');
    
    const imageObserver = new IntersectionObserver((entries, observer) => {
        entries.forEach(entry => {
            if (entry.isIntersecting) {
                const img = entry.target;
                img.src = img.dataset.src;
                img.classList.remove('lazy');
                imageObserver.unobserve(img);
            }
        });
    });
    
    images.forEach(img => imageObserver.observe(img));
}

// Initialize lazy loading
document.addEventListener('DOMContentLoaded', () => {
    lazyLoadImages();
});

// Add search functionality (future enhancement)
function initializeSearch() {
    const searchInput = document.getElementById('search-input');
    if (searchInput) {
        searchInput.addEventListener('input', (e) => {
            const query = e.target.value.toLowerCase();
            // Implementation for search functionality
            console.log('Searching for:', query);
        });
    }
}

// Theme toggle functionality (future enhancement)
function initializeThemeToggle() {
    const themeToggle = document.getElementById('theme-toggle');
    if (themeToggle) {
        themeToggle.addEventListener('click', () => {
            document.body.classList.toggle('dark-theme');
            localStorage.setItem('theme', document.body.classList.contains('dark-theme') ? 'dark' : 'light');
        });
        
        // Load saved theme
        const savedTheme = localStorage.getItem('theme');
        if (savedTheme === 'dark') {
            document.body.classList.add('dark-theme');
        }
    }
}

// Owl flying animation when clicked
function initializeOwlAnimation() {
    const owlImages = document.querySelectorAll('img[src*="8af825b7-fc42-4e0b-8aab-da9bba99b6e0"]');
    
    owlImages.forEach(owl => {
        owl.style.cursor = 'pointer';
        owl.style.transition = 'all 0.3s ease';
        
        owl.addEventListener('click', (e) => {
            e.preventDefault();
            flyOwl(owl);
        });
        
        // Add hover effect
        owl.addEventListener('mouseenter', () => {
            owl.style.transform = 'scale(1.05) rotate(2deg)';
        });
        
        owl.addEventListener('mouseleave', () => {
            owl.style.transform = 'scale(1) rotate(0deg)';
        });
    });
}

function flyOwl(owlElement) {
    // Create flying owl clone
    const flyingOwl = owlElement.cloneNode(true);
    flyingOwl.style.cssText = `
        position: fixed;
        z-index: 9999;
        pointer-events: none;
        width: 80px;
        height: 80px;
        transition: none;
    `;
    
    // Get starting position
    const rect = owlElement.getBoundingClientRect();
    flyingOwl.style.left = rect.left + 'px';
    flyingOwl.style.top = rect.top + 'px';
    
    document.body.appendChild(flyingOwl);
    
    // Original owl animation
    owlElement.style.transform = 'scale(0.8) rotate(10deg)';
    owlElement.style.opacity = '0.7';
    
    setTimeout(() => {
        owlElement.style.transform = 'scale(1) rotate(0deg)';
        owlElement.style.opacity = '1';
    }, 300);
    
    // Flying animation
    const startX = rect.left;
    const startY = rect.top;
    const endX = Math.random() * (window.innerWidth - 100);
    const endY = Math.random() * (window.innerHeight - 100);
    
    // Calculate curve control points
    const controlX1 = startX + (endX - startX) * 0.3 + (Math.random() - 0.5) * 200;
    const controlY1 = startY - 150 - Math.random() * 100;
    const controlX2 = startX + (endX - startX) * 0.7 + (Math.random() - 0.5) * 200;
    const controlY2 = startY - 100 - Math.random() * 150;
    
    // Create curved path animation
    let progress = 0;
    const duration = 2000 + Math.random() * 1000; // 2-3 seconds
    const startTime = performance.now();
    
    function animateOwl(currentTime) {
        progress = (currentTime - startTime) / duration;
        
        if (progress <= 1) {
            // Cubic bezier curve calculation
            const t = progress;
            const invT = 1 - t;
            
            const x = invT * invT * invT * startX +
                     3 * invT * invT * t * controlX1 +
                     3 * invT * t * t * controlX2 +
                     t * t * t * endX;
                     
            const y = invT * invT * invT * startY +
                     3 * invT * invT * t * controlY1 +
                     3 * invT * t * t * controlY2 +
                     t * t * t * endY;
            
            flyingOwl.style.left = x + 'px';
            flyingOwl.style.top = y + 'px';
            
            // Rotation and scale effects
            const rotation = Math.sin(progress * Math.PI * 4) * 20; // Wing flapping effect
            const scale = 0.8 + Math.sin(progress * Math.PI * 2) * 0.2; // Size pulsing
            flyingOwl.style.transform = `rotate(${rotation}deg) scale(${scale})`;
            
            // Fade out at the end
            if (progress > 0.8) {
                flyingOwl.style.opacity = (1 - progress) * 5;
            }
            
            requestAnimationFrame(animateOwl);
        } else {
            // Remove flying owl and create landing effect
            flyingOwl.remove();
            createOwlLandingEffect(endX, endY);
        }
    }
    
    requestAnimationFrame(animateOwl);
    
    // Add sound effect (optional - using Web Audio API)
    playOwlSound();
}

function createOwlLandingEffect(x, y) {
    // Create sparkle effect where owl lands
    for (let i = 0; i < 8; i++) {
        const sparkle = document.createElement('div');
        sparkle.style.cssText = `
            position: fixed;
            left: ${x + 40}px;
            top: ${y + 40}px;
            width: 4px;
            height: 4px;
            background: #a855f7;
            border-radius: 50%;
            pointer-events: none;
            z-index: 9999;
        `;
        
        document.body.appendChild(sparkle);
        
        // Animate sparkles
        const angle = (i / 8) * Math.PI * 2;
        const distance = 30 + Math.random() * 20;
        const endX = x + 40 + Math.cos(angle) * distance;
        const endY = y + 40 + Math.sin(angle) * distance;
        
        sparkle.animate([
            { transform: 'scale(0)', opacity: 1 },
            { transform: 'scale(1)', opacity: 1, offset: 0.3 },
            { transform: `translate(${endX - (x + 40)}px, ${endY - (y + 40)}px) scale(0)`, opacity: 0 }
        ], {
            duration: 800,
            easing: 'cubic-bezier(0.25, 0.46, 0.45, 0.94)'
        }).onfinish = () => sparkle.remove();
    }
    
    // Create a small owl emoji that fades away
    const landingOwl = document.createElement('div');
    landingOwl.textContent = '🦉';
    landingOwl.style.cssText = `
        position: fixed;
        left: ${x + 25}px;
        top: ${y + 25}px;
        font-size: 2rem;
        pointer-events: none;
        z-index: 9999;
    `;
    
    document.body.appendChild(landingOwl);
    
    landingOwl.animate([
        { transform: 'scale(0) rotate(0deg)', opacity: 1 },
        { transform: 'scale(1.2) rotate(5deg)', opacity: 1, offset: 0.5 },
        { transform: 'scale(0) rotate(10deg)', opacity: 0 }
    ], {
        duration: 1500,
        easing: 'ease-out'
    }).onfinish = () => landingOwl.remove();
}

function playOwlSound() {
    // Create a simple "whoosh" sound using Web Audio API
    try {
        const audioContext = new (window.AudioContext || window.webkitAudioContext)();
        const oscillator = audioContext.createOscillator();
        const gainNode = audioContext.createGain();
        
        oscillator.connect(gainNode);
        gainNode.connect(audioContext.destination);
        
        // Create a "whoosh" sound effect
        oscillator.frequency.setValueAtTime(800, audioContext.currentTime);
        oscillator.frequency.exponentialRampToValueAtTime(200, audioContext.currentTime + 0.5);
        
        gainNode.gain.setValueAtTime(0, audioContext.currentTime);
        gainNode.gain.linearRampToValueAtTime(0.1, audioContext.currentTime + 0.1);
        gainNode.gain.exponentialRampToValueAtTime(0.01, audioContext.currentTime + 0.5);
        
        oscillator.start(audioContext.currentTime);
        oscillator.stop(audioContext.currentTime + 0.5);
    } catch (e) {
        // Fallback: no sound if Web Audio API is not supported
        console.log('Audio not supported');
    }
}

// Initialize all features when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
    initializeSearch();
    initializeThemeToggle();
    initializeOwlAnimation();
    
    // Add loading animation
    document.body.classList.add('loaded');
});

// Handle form submissions (if any)
document.addEventListener('submit', (e) => {
    if (e.target.matches('.contact-form')) {
        e.preventDefault();
        // Handle contact form submission
        console.log('Form submitted');
    }
});

// Add to cart functionality for downloads (future enhancement)
function trackDownload(fileName) {
    // Analytics tracking
    if (typeof gtag !== 'undefined') {
        gtag('event', 'download', {
            'event_category': 'engagement',
            'event_label': fileName
        });
    }
}

// Service worker registration for PWA capabilities
if ('serviceWorker' in navigator) {
    window.addEventListener('load', () => {
        navigator.serviceWorker.register('/sw.js')
            .then((registration) => {
                console.log('SW registered: ', registration);
            })
            .catch((registrationError) => {
                console.log('SW registration failed: ', registrationError);
            });
    });
}
